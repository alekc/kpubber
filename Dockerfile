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

RUN go mod download

# This image builds the weavaite server
FROM build_base AS server_builder
# Here we copy the rest of the source code
COPY . .
# And compile the project
RUN CGO_ENABLED=1 go build -a -tags netgo -ldflags '-w -extldflags "-static"' -o /bin/kpubber main.go

FROM alpine AS final
RUN apk add ca-certificates && addgroup -S app && adduser -S -G app app
COPY --from=server_builder /bin/kpubber /bin/kpubber

USER app

ENTRYPOINT ["/bin/kpubber"]

