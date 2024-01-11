FROM golang:1.21-alpine AS build
ARG version
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build \
  -mod=readonly \
  -trimpath \
  -ldflags "-s -w -X github.com/n6g7/nomtail/internal/version.Version=$version" \
  ./cmd/nomtail

FROM alpine:3.19
COPY --from=build /src/nomtail /usr/bin
CMD ["/usr/bin/nomtail"]
