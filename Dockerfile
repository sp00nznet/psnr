FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod .
COPY server/ server/
RUN cd server && CGO_ENABLED=0 go build -o /psnr .

FROM alpine:3.20
COPY --from=build /psnr /usr/local/bin/psnr
EXPOSE 36100 36101
ENTRYPOINT ["psnr", "-http", "0.0.0.0:36101"]
