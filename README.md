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

### Updating the AMI

1. Update the leonardo script in the `mona-lisa` repository
2. Run Packer to create a new AMI
3. Take the new AMI ID and update the terraform repository with that AMI
4. Cycle the cluster through the UI to start new instances with the new AMI

## Monitoring

Prometheus and Grafana are jobs configured to run in the `nomadic-jobs` repo; The servers that run the Prometheus service have an EFS mount to write that data. In order to connect to these things use `./bin/start-prod-tunnel` and connect via a local ssh tunnel to the default ports for these services.

Things are being stored in Prometheus:

1. The `s3tp` server application is exposing go metrics via the gosdk;
2. The `s3tp` server is also exposing custom metrics around number of connected clients
3. Prometheus is reading the exposed metrics from nomad which includes things like memory utilization for the services
4. Prometheus monitors itself

## Web Components

The `s3tp-api` project contains a rails application meant to interact with the heroku provisioning service. As part of working with heroku apps are responsible for showing a dashboard to their users. This dashboard may also live in the `s3tp-api` repository but I'm not quite sure at the time of this writing.

The `s3tp-web` application contains a simple rails app that shows an IFRAME to the product hunt page. `s3tp.io` seems to redirect to this page.

## README TODO

Add details about doing remote profiling of the server
