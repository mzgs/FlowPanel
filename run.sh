#!/bin/bash

go run cmd/flowpanel/main.go serve &
(cd web/panel && npm run dev) &

wait
