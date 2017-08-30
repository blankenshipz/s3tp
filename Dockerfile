FROM golang

RUN mkdir -p /go/src/app
WORKDIR /go/src/app

COPY . /go/src/app

RUN go-wrapper download && go-wrapper install

CMD ["go", "test"]
