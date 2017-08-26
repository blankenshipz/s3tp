FROM golang

RUN mkdir -p /go/src/app
WORKDIR /go/src/app

RUN \
  mkdir -p /go/src/github.com/pkg && \
  ln -sfv /go/src/app /go/src/github.com/pkg/sftp

COPY . /go/src/app

RUN go-wrapper download && go-wrapper install && go get github.com/stretchr/testify/assert

CMD ["go", "test"]
