First run
Step 1: ./signalgateway -phone +{phonenumber} -register
Step 2: enter code when you get it.
Step 3: kill it
Step 4: ./signalgateway -phone +{phonenumber}
Step 5: Profit?

Options
-phone full phone number required
-register only need for first run to register the number
-port change the default port number which is :8080
-webhook upload to server

API Usage
POST http://localhost:8080/signal
	Fields
	contact: phone number to send to
	message: message to send
	attachment: file upload

curl -F "contact=+{number}" -F "attachment=@test.jpg" http://localhost:8080/signal
