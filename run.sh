#!/bin/bash

(cd web/panel && npm run build)

go run ./cmd/flowpanel serve &
(cd web/panel && npm run dev) &

wait
