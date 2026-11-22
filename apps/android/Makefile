# Builds the Go core into app/libs/warpcore.aar. Needs: Go, the Android NDK, and gomobile
#   android sdk install ndk/30.0.16248370 && go install golang.org/x/mobile/cmd/gomobile@latest
NDK ?= $(HOME)/Android/Sdk/ndk/30.0.16248370
ABIS ?= android/arm64,android/arm
GOBIN := $(shell go env GOPATH)/bin

.PHONY: core
core:
	cd ../../cli && ANDROID_HOME=$(HOME)/Android/Sdk ANDROID_NDK_HOME=$(NDK) PATH="$(PATH):$(GOBIN)" \
	  gomobile bind -target=$(ABIS) -androidapi 29 -javapkg=info.warplabs.warpdrop.core -trimpath \
	  -ldflags="-s -w -checklinkname=0" -o ../apps/android/app/libs/warpcore.aar ./mobile
	python3 scripts/go_licenses.py ../../cli app/aboutlibraries
