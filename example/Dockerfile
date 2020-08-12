FROM golang:1.14

VOLUME /certs
VOLUME /www
EXPOSE 443
WORKDIR /
COPY . .
RUN go get "github.com/IRelaxxx/quic-go"
RUN go get "github.com/IRelaxxx/quic-go/http3"
RUN go get "github.com/IRelaxxx/quic-go/logging"
RUN go get "github.com/IRelaxxx/quic-go/qlog"
RUN go get "github.com/IRelaxxx/quic-go/quictrace"
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o main .


#ADD main /main
# "-bind="0.0.0.0"
CMD ["/main", "-certpath=/certs/", "-www=/www"]
