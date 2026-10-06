# build stage
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN go build -o /dashboard-server .

# run stage
FROM alpine:3.20
WORKDIR /app
COPY --from=build /dashboard-server .
EXPOSE 8080
ENTRYPOINT ["/app/dashboard-server"]
