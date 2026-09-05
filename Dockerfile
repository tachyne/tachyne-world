# tachyne-world: the tachyne world engine + its cluster packaging in ONE repo
# (the former minecraft/server was folded in here 2026-07-09). This Dockerfile
# builds the LOCAL Go source into a scratch world pod. CI (ci.yml) is the
# test gate — gofmt/vet/test and a -race job run on every push — so the image
# build just builds; re-running the suite here doubled every image's build
# time for no extra assurance.
FROM golang:1.26-alpine AS build
ENV RUN apk add --no-cache git
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go vet ./...
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/world ./cmd/server

FROM scratch
COPY --from=build /out/world /world
# All CWD-relative persistence (world.gob, chunks/, players.json) lands on the
# PVC mounted here.
WORKDIR /var/world
USER 1000:1000
EXPOSE 25565
ENTRYPOINT ["/world"]
