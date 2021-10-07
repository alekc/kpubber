# Base build image

FROM golang:1.16-alpine AS build_base

ARG CI_JOB_TOKEN

# Install some dependencies needed to build the project
RUN apk add bash ca-certificates git gcc g++ libc-dev

WORKDIR /build/

# Force the go compiler to use modules
ENV GO111MODULE=on

# We want to populate the module cache based on the go.{mod,sum} files.
COPY go.mod .
COPY go.sum .

#This is the ‘magic’ step that will download all the dependencies that are specified in
# the go.mod and go.sum file.

# Because of how the layer caching system works in Docker, the go mod download
# command will _ only_ be re-run when the go.mod or go.sum file change
# (or when we add another docker instruction this line)
RUN go mod download

# This image builds the weavaite server
FROM build_base AS server_builder
# Here we copy the rest of the source code
COPY . .
# And compile the project
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -a -tags netgo -ldflags '-w -extldflags "-static"' -o /bin/kpubber ./cmd/kpubber.go

FROM alpine AS final
RUN apk add ca-certificates && addgroup -S app && adduser -S -G app app
COPY --from=server_builder /bin/kpubber /bin/kpubber

USER app

ENTRYPOINT ["/bin/kpubber"]

