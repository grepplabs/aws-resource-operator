# Build the manager binary
FROM golang:1.2 as builder

# Copy in the go src
WORKDIR /go/src/github.com/grepplabs/aws-resource-operator
COPY pkg/    pkg/
COPY cmd/    cmd/
COPY vendor/ vendor/

# Build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o manager github.com/grepplabs/aws-resource-operator/cmd/manager

# Copy the controller-manager into a thin image
FROM ubuntu:latest
WORKDIR /
COPY --from=builder /go/src/github.com/grepplabs/aws-resource-operator/manager .
ENTRYPOINT ["/manager"]
