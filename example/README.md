# Khatru Payments Example

Build the Relay

- Configure the relay using the .env.example
- Payment data and expiration are stored in the "data" directory, in two files: charge_mappings.json and paid_access.json
- set the PAYMENT_PROVIDER and ensure your backend api or node is running. 

```sh
 go build -o example-relay example-relay.go
```

Run the client

The first time you run the client it will generate a keypair and store it in test-keypair.txt
It will ask for an invoice from the relay, and requires manual payment. 
Once it has been paid, hit enter. 
Client should have access to post to relay on next interation


```sh
 go run test-client.go connect
 ```


 
 # Run the relay server

```sh
cd khatru-payments/example
go run -tags=relay relay/example-relay.go
```

# Run the test client (in another terminal)

```sh
cd khatru-payments/example  
go run -tags=client client/test-client.go stats
go run -tags=client client/test-client.go connect
go run -tags=client client/test-client.go test-payment
```
