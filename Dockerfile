# This is a multi-stage build.

# build stage
FROM golang:1.11 AS builder
WORKDIR /go/src/github.com/mrichman/hargo
COPY . .
RUN go get -d -v
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o app .

# final stage
FROM scratch
WORKDIR /root/
COPY --from=builder /go/src/github.com/mrichman/hargo/app .

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

CMD ["./app"]