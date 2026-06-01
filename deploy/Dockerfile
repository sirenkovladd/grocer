# Stage 1: Build the client
FROM oven/bun:1 as client
WORKDIR /app
COPY client ./client
COPY package.json bun.lock tsconfig.json .
RUN bun install --frozen-lockfile
RUN bun build --production --outdir=dist ./client/index.html

# Stage 2: Build the server
FROM golang:1.25.1 as server
WORKDIR /app
ENV CGO_ENABLED=0
COPY go.mod go.sum .
RUN go mod download
COPY cmd cmd
COPY internal internal
COPY proto proto
COPY --from=client /app/dist ./dist
ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown
RUN go build -ldflags "-s -w -X 'main.GitCommit=$GIT_COMMIT' -X 'main.BuildTime=$BUILD_TIME'" -o /app/app ./cmd/server

# Stage 3: Final image
FROM gcr.io/distroless/static-debian12
WORKDIR /app
COPY --from=server /app/app .
EXPOSE 8080
CMD ["./app"]
