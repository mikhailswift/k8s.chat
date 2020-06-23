FROM golang:1.14.4-alpine as build

WORKDIR /src
RUN mkdir bin

COPY ./server /src/server
COPY ./go.mod /src/go.mod
COPY ./go.sum /src/go.sum

RUN go build -o bin/k8schatserver ./server/

FROM alpine
CMD ["k8schatserver"]
EXPOSE 8080/tcp
COPY --from=build /src/bin/k8schatserver /usr/bin/
