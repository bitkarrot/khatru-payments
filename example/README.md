# Khatru Payments Example

Build the Relay

```sh
 go build -o example-relay example-relay.go
```

Run the client 
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
