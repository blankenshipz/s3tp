# About (DEPRECATED)

S3TP is an SFTP Server that does pass-through to S3 when provided credentials for an
IAM user. This project was deprecated after AWS released a managed SFTP service.

## Setup

Build the image and run the migrations against the local database

```bash
docker-compose build && \
docker-compose run --rm bin /bin/bash -c "migrate -source file://migrate -database postgres://postgres:postgres@postgres:5432/postgres?sslmode=disable up"
```

## Run the Server

`docker-compose up` will compile the binary and perform `go run` since we're using `up` here the ports will be exposed so that you can actually use connect to the server.

### Connect

Use credentials for an IAM user (AWS_ACCESS_KEY/AWS_SECRET_KEY) to connect

```
> sftp <aws_access_key>@localhost
aws_access_key@localhost's password: <aws_secret_key>
```

## README TODO

Add details about doing remote profiling of the server
