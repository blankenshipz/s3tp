# About

S3TP is an SFTP Server that does pass-through to S3 when provided credentials for an
IAM user.

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

## Deploying

1. Build a new image and push it to ECR
2. Setup an SSH tunnel to production (`./bin/start-prod-tunnel`)
3. Use the `nomad` cli to update the job to the latest image; jobs are defined in the [hunting-gathering](https://gitlab.com/lux-software/hunting-gathering) repo
4. Stop the SSH tunnel (`./bin/stop-prod-tunnel`)

## Infrastructure

Currently the infrastructure is defined in terraform inside of the [genesis-cave](https://gitlab.com/lux-software/genesis-cave) repository.
The AMI's used are provisioned by Packer in a repository called [mona-lisa](https://gitlab.com/lux-software/mona-lisa)




