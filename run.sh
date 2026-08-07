#!/bin/bash

(cd web/panel && npm run build) || exit

go run ./cmd/flowpanel serve &
(cd web/panel && npm run dev) &

wait
