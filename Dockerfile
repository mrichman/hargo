# This is a multi-stage build.

# build stage
FROM golang:1.27.1 AS builder
WORKDIR /go/src/hargo

# Fetch through the module proxy rather than GOPROXY=direct. Going direct pulls
# from each upstream host, which is slower and prone to truncated transfers
# ("N bytes of body are still expected") on constrained networks.
ENV GOPROXY=https://proxy.golang.org,direct

# Resolve dependencies before copying the source, so editing a .go file reuses
# the cached download layer instead of re-fetching every module.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION
ARG HASH
ARG DATE

ENV CGO_ENABLED=0
ENV GOOS=linux

RUN go build -ldflags "-s -w -X main.Version=$VERSION -X main.CommitHash=$HASH -X 'main.CompileDate=$DATE'" -o hargo ./cmd/hargo

# final stage
FROM scratch

WORKDIR /
ENV PATH=/

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /go/src/hargo/hargo /

# Metadata params
ARG VERSION
ARG BUILD_DATE
ARG VCS_URL
ARG VCS_REF
ARG NAME
ARG VENDOR

# Metadata
LABEL org.label-schema.build-date=$BUILD_DATE \
      org.label-schema.name=$NAME \
      org.label-schema.description="hargo" \
      org.label-schema.url="https://markrichman.com" \
      org.label-schema.vcs-url=https://github.com/mrichman/$VCS_URL \
      org.label-schema.vcs-ref=$VCS_REF \
      org.label-schema.vendor=$VENDOR \
      org.label-schema.version=$VERSION \
      org.label-schema.docker.schema-version="1.0" \
      org.label-schema.docker.cmd="docker run --rm hargo"

CMD ["./hargo"]
