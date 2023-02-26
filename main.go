package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/jckimble/smsprovider"
	"github.com/jckimble/smsprovider/signal"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	ossignal "os/signal"
	"strconv"
	"strings"
	"sync"
	"time"
)

var number = flag.String("phone", "", "phone number to use")
var register = flag.Bool("register", false, "register and wait for code")
var port = flag.String("port", ":8080", "port to listen to")
var webhook = flag.String("webhook", "", "webhook to send incoming messages")

func main() {
	flag.Parse()
	httpclient := http.Client{
		Timeout: time.Second * 30,
	}
	signalsms := &signal.Signal{
		Handler: func(m smsprovider.Message) {
			if *webhook == "" {
				return
			}
			body := &bytes.Buffer{}
			writer := multipart.NewWriter(body)
			for i, attach := range m.Attachments() {
				ext, err := mime.ExtensionsByType(attach.GetMimeType())
				if err != nil {
					log.Printf("%s", err)
					return
				}
				part, err := writer.CreateFormFile("attachments", "attachments"+strconv.Itoa(i+1)+"."+ext[0])
				if err != nil {
					log.Printf("%s", err)
					return
				}
				reader, _ := attach.GetReader()
				if _, err := io.Copy(part, reader); err != nil {
					log.Printf("%s", err)
					return
				}
			}
			writer.WriteField("source", m.Source())
			writer.WriteField("message", m.Message())
			if err := writer.Close(); err != nil {
				log.Printf("%s", err)
				return
			}
			req, err := http.NewRequest("POST", *webhook, body)
			if err != nil {
				log.Printf("%s", err)
				return
			}
			req.Header.Set("Content-Type", writer.FormDataContentType())
			resp, err := httpclient.Do(req)
			if err != nil {
				log.Printf("%s", err)
				return
			}
			defer resp.Body.Close()
		},
		GetPhoneNumberFunc: func() (string, error) {
			if *number == "" {
				return "", fmt.Errorf("phone number must be provided")
			} else if !strings.HasPrefix(*number, "+") {
				return "", fmt.Errorf("phone number must be in international format +{countrycode}{number}")
			}
			return *number, nil
		},
		StorageDir: "./.signal",
	}
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		if err := signalsms.Setup(); err != nil {
			log.Fatal(err)
		}
	}()
	if *register {
		var text string
		for {
			reader := bufio.NewReader(os.Stdin)
			fmt.Print("Enter Code: ")
			text, _ = reader.ReadString('\n')
			text = strings.TrimSpace(text)
			if text != "" && len(text) == 7 {
				break
			} else if text == "" {
				fmt.Println("Code can't be empty")
			} else if len(text) != 7 {
				fmt.Println("Code must be in format 000-000")
			}
		}
		signalsms.SetVerificationCode(text)
	}
	r := mux.NewRouter()
	r.HandleFunc("/signal", SendFunc(signalsms)).Methods("POST")
	srv := &http.Server{
		Addr:    *port,
		Handler: r,
	}
	go func() {
		defer wg.Done()
		if err := srv.ListenAndServe(); err != nil {
			log.Fatal(err)
		}
	}()
	go func() {
		defer wg.Done()
		c := make(chan os.Signal, 1)
		ossignal.Notify(c, os.Interrupt)
		<-c
		ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
		srv.Shutdown(ctx)
		signalsms.Shutdown()
	}()
	wg.Wait()
}
func SendFunc(p smsprovider.Provider) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		r.ParseMultipartForm(0)
		if r.FormValue("contact") == "" || (!strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") && r.FormValue("message") == "") {
			res := APIResponse{
				Code:    400,
				Message: "Missing Required Fields",
			}
			if err := res.Write(w); err != nil {
				log.Printf("%s", err)
			}
			return
		}
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") { //attachment
			if ap, ok := interface{}(p).(smsprovider.AttachmentProvider); ok {
				file, _, err := r.FormFile("attachment")
				if err != nil {
					log.Printf("%s", err)
					res := APIResponse{
						Code:    500,
						Message: "Unable to send Attachment",
					}
					if err := res.Write(w); err != nil {
						log.Printf("%s", err)
					}
					return
				}
				defer file.Close()
				if err := ap.SendAttachment(r.FormValue("contact"), r.FormValue("message"), file); err != nil {
					log.Printf("%s", err)
					res := APIResponse{
						Code:    500,
						Message: "Unable to send Attachment",
					}
					if err := res.Write(w); err != nil {
						log.Printf("%s", err)
					}
					return
				}
				res := APIResponse{
					Code:    200,
					Message: "Attachment Sent",
				}
				if err := res.Write(w); err != nil {
					log.Printf("%s", err)
				}
			} else {
				res := APIResponse{
					Code:    403,
					Message: "Provider doesn't support attachments",
				}
				if err := res.Write(w); err != nil {
					log.Printf("%s", err)
				}
			}
		} else {
			if err := p.SendMessage(r.FormValue("contact"), r.FormValue("message")); err != nil {
				log.Printf("%s", err)
				res := APIResponse{
					Code:    500,
					Message: "Unable to send Message",
				}
				if err := res.Write(w); err != nil {
					log.Printf("%s", err)
				}
				return
			}
			res := APIResponse{
				Code:    200,
				Message: "Message Sent",
			}
			if err := res.Write(w); err != nil {
				log.Printf("%s", err)
			}
		}
	}
}

type APIResponse struct {
	Code    int    `json:"code"`
	Message string `json:"msg"`
}

func (ar APIResponse) Write(w http.ResponseWriter) error {
	w.WriteHeader(ar.Code)
	return json.NewEncoder(w).Encode(ar)
}
