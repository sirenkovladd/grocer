# Stage 1: Build the client
FROM oven/bun:1 as client
WORKDIR /app
COPY client ./client
COPY package.json bun.lock .
RUN bun install --frozen-lockfile
RUN bun build --production --outdir=dist ./client/index.html

# Stage 2: Build the server
FROM golang:1.25.1 as server
WORKDIR /app
ENV CGO_ENABLED=0
COPY assets.go go.mod go.sum .
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY src src
COPY server server
COPY cli cli
# RUN ls -la & sleep 12
COPY --from=client /app/dist ./dist
ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown
ENV GOCACHE=/go-build-cache
RUN --mount=type=cache,target=/go-build-cache go build -ldflags "-s -w -X 'main.GitCommit=$GIT_COMMIT' -X 'main.BuildTime=$BUILD_TIME'" -tags prod -o /app/app ./cli/server/server.go

# Stage 3: Final image
FROM gcr.io/distroless/static-debian12
WORKDIR /app
COPY ./server/migrations ./server/migrations
COPY --from=server /app/app .
EXPOSE 8080
CMD ["./app"]
