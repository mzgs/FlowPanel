#!/bin/bash

go run cmd/flowpanel/main.go &
(cd web/panel && npm run dev) &

wait