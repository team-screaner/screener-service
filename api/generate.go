package api

//go:generate python3 ../scripts/contract.py
//go:generate go tool oapi-codegen -config oapi-codegen.yaml openapi.yaml

//go:generate python3 ../scripts/bridge.py
