FROM golang:1.14

VOLUME /certs
VOLUME /www
EXPOSE 443
WORKDIR /
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o main .


#ADD main /main
# "-bind="0.0.0.0"
CMD ["/main", "-certpath=/certs/", "-www=/www"]
