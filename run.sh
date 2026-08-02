#!/bin/bash

go run ./cmd/flowpanel serve &
(cd web/panel && npm run dev) &

wait
