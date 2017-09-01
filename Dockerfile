FROM golang

RUN mkdir -p /go/src/app
WORKDIR /go/src/app

COPY . /go/src/app

RUN go-wrapper download && go-wrapper install
RUN go get github.com/stretchr/testify/assert

CMD ["go", "test", "-v"]
