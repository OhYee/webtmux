FROM node:22-alpine AS frontend
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci
COPY resources ./resources
RUN npm run bundle

FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /src/bindata/static/js/bundle.js ./bindata/static/js/bundle.js
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/webtmux .

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tmux && \
    adduser -D -h /home/webtmux webtmux
COPY --from=build /out/webtmux /usr/local/bin/webtmux
USER webtmux
WORKDIR /home/webtmux
EXPOSE 8080
ENTRYPOINT ["webtmux"]
CMD ["-w", "tmux", "new-session", "-A", "-s", "main"]
