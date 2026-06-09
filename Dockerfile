FROM golang:1.26-alpine
ENV TERM xterm-256color
ENV COLORTERM truecolor
RUN go install github.com/mansueli/world-cup-2026-cli-dashboard@latest
ENTRYPOINT world-cup-2026-cli-dashboard
