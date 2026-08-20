FROM golang:1.26.5-bookworm AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY pkg ./pkg
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/c2k ./cmd/c2k
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/demo-kernel ./kernels/demo

FROM scratch
COPY --from=build /out/c2k /c2k
COPY --from=build --chmod=0500 /out/demo-kernel /opt/c2k/bin/demo-kernel
USER 65532:65532
ENTRYPOINT ["/c2k"]
