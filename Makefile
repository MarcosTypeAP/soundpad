LINUX_CC = gcc
LINUX_CGO_CFLAGS = -I./third_party/linux/include
LINUX_CGO_LDFLAGS = -L./third_party/linux/lib -Wl,-Bstatic -lportaudio -Wl,-Bdynamic -lpulse
LINUX_PKG_CONFIG_PATH = ${CURDIR}/third_party/linux/lib/pkgconfig

WINDOWS_CC = x86_64-w64-mingw32-gcc
WINDOWS_CGO_CFLAGS = -I./third_party/windows/include
WINDOWS_CGO_LDFLAGS = -L./third_party/windows/lib -Wl,-Bstatic -lportaudio -Wl,-Bdynamic
WINDOWS_PKG_CONFIG_PATH = ${CURDIR}/third_party/windows/lib/pkgconfig

FLAGS_LINUX_DEV = \
	CGO_ENABLED=1 \
	CC="${LINUX_CC}" \
	CGO_CFLAGS="${LINUX_CGO_CFLAGS}" \
	CGO_LDFLAGS="${LINUX_CGO_LDFLAGS}" \
	PKG_CONFIG_PATH="${LINUX_PKG_CONFIG_PATH}" \
	GOOS=linux \
	GOARCH=amd64

FLAGS_LINUX_RELEASE = \
	CGO_ENABLED=1 \
	CC="${LINUX_CC}" \
	CGO_CFLAGS="-O3 ${LINUX_CGO_CFLAGS}" \
	CGO_LDFLAGS="-s ${LINUX_CGO_LDFLAGS}" \
	PKG_CONFIG_PATH="${LINUX_PKG_CONFIG_PATH}" \
	GOOS=linux \
	GOARCH=amd64

FLAGS_WINDOWS_DEV = \
	CGO_ENABLED=1 \
	CC=${WINDOWS_CC} \
	CGO_CFLAGS="${WINDOWS_CGO_CFLAGS}" \
	CGO_LDFLAGS="${WINDOWS_CGO_LDFLAGS}" \
	PKG_CONFIG_PATH="${WINDOWS_PKG_CONFIG_PATH}" \
	GOOS=windows \
	GOARCH=amd64

FLAGS_WINDOWS_RELEASE = \
	CGO_ENABLED=1 \
	CC=${WINDOWS_CC} \
	CGO_CFLAGS="-O3 ${WINDOWS_CGO_CFLAGS}" \
	CGO_LDFLAGS="-s ${WINDOWS_CGO_LDFLAGS}" \
	PKG_CONFIG_PATH="${WINDOWS_PKG_CONFIG_PATH}" \
	GOOS=windows \
	GOARCH=amd64

GO_FLAGS_LINUX_RELEASE = -trimpath -ldflags="-s -w"
GO_FLAGS_WINDOWS_RELEASE = -trimpath -ldflags="-s -w -H=windowsgui"

RNNOISE_COMMIT_HASH=70f1d256acd4b34a572f999a05c87bf00b67730d
PORTAUDIO_COMMIT_HASH=d5b81b82f13ae8498f02e27595aa9c50ab2623db

.PHONY: linux-edit
linux-edit:
	$(FLAGS_LINUX_DEV) \
	nvim main.go "+nmap <buffer> <Leader>x :!make linux-dev-rece<CR>" "+normal g'\""

.PHONY: windows-edit
windows-edit:
	$(FLAGS_WINDOWS_DEV) \
	nvim main.go "+nmap <buffer> <Leader>x :!make windows-dev-race<CR>" "+normal g'\""

.PHONY: linux-dev
linux-dev: third_party/linux
	$(FLAGS_LINUX_DEV) \
	go build -tags pprof -o build/soundpad

	SOUNDPAD_DEV=1 build/soundpad

.PHONY: linux-dev-race
linux-dev-race: third_party/linux
	$(FLAGS_LINUX_DEV) \
	SOUNDPAD_DEV=1 \
	GORACE="halt_on_error=1" \
	go run -race -gcflags=all=-d=checkptr=0 . # portaudio breaks otherwise

.PHONY: linux-dev-debug
linux-dev-debug: third_party/linux
	$(FLAGS_LINUX_DEV) \
	SOUNDPAD_DEV=1 \
	dlv debug .

.PHONY: linux-release
linux-release: third_party/linux
	$(FLAGS_LINUX_RELEASE) \
	go build -tags noassert $(GO_FLAGS_LINUX_RELEASE) -o build/soundpad

	build/soundpad

.PHONY: linux-release-dev
linux-release-dev: third_party/linux
	$(FLAGS_LINUX_RELEASE) \
	go build $(GO_FLAGS_LINUX_RELEASE) -o build/soundpad

	build/soundpad

.PHONY: windows-run
windows-run: # for vm
	SOUNDPAD_DEV=1 GORACE=halt_on_error=1 build/Soundpad.exe

.PHONY: windows-dev
windows-dev: third_party/windows
	$(FLAGS_WINDOWS_DEV) \
	go build -o build/Soundpad.exe

.PHONY: windows-dev-race
windows-dev-race: third_party/windows
	$(FLAGS_WINDOWS_DEV) \
	go build -race -gcflags=all=-d=checkptr=0 -o build/Soundpad.exe

.PHONY: windows-release
windows-release: third_party/windows
	# CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc GOOS=windows GOARCH=amd64 go build -ldflags "-s -w -H=windowsgui -extldflags '-static -lsetupapi'" -o build/tally.exe
	$(FLAGS_WINDOWS_RELEASE) \
	go build -tags noassert $(GO_FLAGS_WINDOWS_RELEASE) -o build/Soundpad.exe

.PHONY: windows-release-dev
windows-release-dev: third_party/windows
	# CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc GOOS=windows GOARCH=amd64 go build -ldflags "-s -w -H=windowsgui -extldflags '-static -lsetupapi'" -o build/tally.exe
	$(FLAGS_WINDOWS_RELEASE) \
	go build $(GO_FLAGS_WINDOWS_RELEASE) -o build/Soundpad.exe

third_party/linux:
	mkdir -p third_party/linux
	mkdir -p deps_build
	rm -fr deps_build/*

	git clone https://github.com/xiph/rnnoise.git deps_build/rnnoise && \
	cd deps_build/rnnoise && \
	git reset --hard ${RNNOISE_COMMIT_HASH} && \
	./autogen.sh && \
	./configure \
		--disable-shared --enable-static \
		--disable-doc --disable-examples \
		--enable-x86-rtcd \
		--prefix=${CURDIR}/third_party/linux && \
	make install

	git clone https://github.com/PortAudio/portaudio.git deps_build/portaudio && \
	cd deps_build/portaudio && \
	git reset --hard ${PORTAUDIO_COMMIT_HASH} && \
	./configure \
		--disable-shared --enable-static \
		--without-audioio --without-sndio --without-jack --without-oss --without-asihpi --without-winapi --without-alsa \
		--with-pulseaudio \
		--prefix=${CURDIR}/third_party/linux && \
	make install

	rm -fr deps_build
	go clean -cache

tools/rsrc:
	GOBIN=${CURDIR}/tools go install github.com/akavel/rsrc@latest

rsrc_windows_amd64.syso: tools/rsrc
	# convert assets/icons/soundpad.png -colors 256 assets/soundpad.ico
	tools/rsrc -ico assets/soundpad.ico -arch amd64 -o rsrc_windows_amd64.syso

third_party/windows: rsrc_windows_amd64.syso
	mkdir -p third_party/windows
	mkdir -p deps_build
	rm -fr deps_build/*

	git clone https://github.com/xiph/rnnoise.git deps_build/rnnoise && \
	cd deps_build/rnnoise && \
	git reset --hard ${RNNOISE_COMMIT_HASH} && \
	./autogen.sh && \
	./configure \
        --disable-shared --enable-static \
        --disable-doc --disable-examples \
        --host=x86_64-w64-mingw32 \
        --enable-x86-rtcd \
        --prefix=${CURDIR}/third_party/windows && \
	make install

	git clone https://github.com/PortAudio/portaudio.git deps_build/portaudio && \
	cd deps_build/portaudio && \
	git reset --hard ${PORTAUDIO_COMMIT_HASH} && \
	./configure \
		--disable-shared --enable-static \
        --without-audioio --without-sndio --without-jack --without-oss --without-asihpi --without-alsa --without-pulseaudio \
		--with-winapi=wmme \
        --host=x86_64-w64-mingw32 \
		--prefix=${CURDIR}/third_party/windows && \
	make install

	rm -fr deps_build
	go clean -cache
