FROM golang

RUN mkdir -p /go/src/app
WORKDIR /go/src/app

COPY . /go/src/app

RUN \
  go get -u github.com/golang/dep/cmd/dep && \
  dep ensure

CMD ["go", "test", "-v"]
