LINUX_CC := gcc
LINUX_CGO_CFLAGS := -I./third_party/linux/include
LINUX_CGO_LDFLAGS := -L./third_party/linux/lib -Wl,-Bstatic -lportaudio -Wl,-Bdynamic -lpulse
LINUX_PKG_CONFIG_PATH := ${CURDIR}/third_party/linux/lib/pkgconfig

WINDOWS_CC := x86_64-w64-mingw32-gcc
WINDOWS_CGO_CFLAGS := -I./third_party/windows/include
WINDOWS_CGO_LDFLAGS := -L./third_party/windows/lib -Wl,-Bstatic -lportaudio -Wl,-Bdynamic
WINDOWS_PKG_CONFIG_PATH := ${CURDIR}/third_party/windows/lib/pkgconfig

FLAGS_LINUX_DEV := \
	CGO_ENABLED=1 \
	CC="${LINUX_CC}" \
	CGO_CFLAGS="${LINUX_CGO_CFLAGS}" \
	CGO_LDFLAGS="${LINUX_CGO_LDFLAGS}" \
	PKG_CONFIG_PATH="${LINUX_PKG_CONFIG_PATH}" \
	GOOS=linux \
	GOARCH=amd64

FLAGS_LINUX_RELEASE := \
	CGO_ENABLED=1 \
	CC="${LINUX_CC}" \
	CGO_CFLAGS="-O3 ${LINUX_CGO_CFLAGS}" \
	CGO_LDFLAGS="-s ${LINUX_CGO_LDFLAGS}" \
	PKG_CONFIG_PATH="${LINUX_PKG_CONFIG_PATH}" \
	GOOS=linux \
	GOARCH=amd64

FLAGS_WINDOWS_DEV := \
	CGO_ENABLED=1 \
	CC=${WINDOWS_CC} \
	CGO_CFLAGS="${WINDOWS_CGO_CFLAGS}" \
	CGO_LDFLAGS="${WINDOWS_CGO_LDFLAGS}" \
	PKG_CONFIG_PATH="${WINDOWS_PKG_CONFIG_PATH}" \
	GOOS=windows \
	GOARCH=amd64

FLAGS_WINDOWS_RELEASE := \
	CGO_ENABLED=1 \
	CC=${WINDOWS_CC} \
	CGO_CFLAGS="-O3 ${WINDOWS_CGO_CFLAGS}" \
	CGO_LDFLAGS="-s ${WINDOWS_CGO_LDFLAGS}" \
	PKG_CONFIG_PATH="${WINDOWS_PKG_CONFIG_PATH}" \
	GOOS=windows \
	GOARCH=amd64

GO_FLAGS_LINUX_RELEASE := -trimpath -ldflags="-s -w"
GO_FLAGS_WINDOWS_RELEASE := -trimpath -ldflags="-s -w -H=windowsgui"

RNNOISE_COMMIT_HASH := 70f1d256acd4b34a572f999a05c87bf00b67730d
PORTAUDIO_COMMIT_HASH := d5b81b82f13ae8498f02e27595aa9c50ab2623db
LAME_VERSION := 3.100
FFMPEG_VERSION_TAG := n8.1.1

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
	$(FLAGS_WINDOWS_RELEASE) \
	go build -tags noassert $(GO_FLAGS_WINDOWS_RELEASE) -o build/Soundpad.exe

.PHONY: windows-release-dev
windows-release-dev: third_party/windows
	$(FLAGS_WINDOWS_RELEASE) \
	go build $(GO_FLAGS_WINDOWS_RELEASE) -o build/Soundpad.exe

deps_build/rnnoise:
	rm -fr deps_build/rnnoise
	mkdir -p deps_build/rnnoise

	cd deps_build/rnnoise && \
	git init && \
	git remote add origin https://github.com/xiph/rnnoise.git && \
	git fetch --depth=1 origin ${RNNOISE_COMMIT_HASH} && \
	git switch -d FETCH_HEAD && \
	./autogen.sh

deps_build/portaudio:
	rm -fr deps_build/portaudio
	mkdir -p deps_build/portaudio

	cd deps_build/portaudio && \
	git init && \
	git remote add origin https://github.com/PortAudio/portaudio.git && \
	git fetch --depth=1 origin ${PORTAUDIO_COMMIT_HASH} && \
	git switch -d FETCH_HEAD

deps_build/lame:
	rm -fr deps_build/lame
	mkdir -p deps_build/lame

	cd deps_build/lame && \
	curl -fsSL https://sitsa.dl.sourceforge.net/project/lame/lame/${LAME_VERSION}/lame-${LAME_VERSION}.tar.gz \
		| tar -xzf - --strip-components=1

deps_build/ffmpeg:
	rm -fr deps_build/ffmpeg
	mkdir -p deps_build

	git clone https://git.ffmpeg.org/ffmpeg.git \
		--depth=1 --branch=${FFMPEG_VERSION_TAG} deps_build/ffmpeg

FFMPEG_COMMON_BUILD_FLAGS := \
	--disable-all \
	--disable-shared \
	--disable-doc \
	--disable-programs \
	--enable-ffmpeg \
	--enable-avcodec \
	--enable-avformat \
	--enable-avfilter \
	--enable-swresample \
	--enable-demuxer=mov,matroska,wav,mp3,flac,ogg,aac,pcm_f32le,pcm_u8 \
	--enable-decoder=aac,mp3,vorbis,opus,flac,wav,m4a,pcm_f32le,pcm_s16le,pcm_s24le,pcm_u8 \
	--enable-encoder=libmp3lame,pcm_f32le,pcm_s16le,pcm_s8 \
	--enable-muxer=mp3,pcm_f32le,pcm_s16le,pcm_s8 \
	--enable-libmp3lame \
	--enable-parser=aac,mpegaudio,opus,vorbis,flac \
	--enable-filter=aresample,aformat \
	--enable-protocol=file,pipe
	
THIRD_PARTY_LINUX_RNNOISE_FILES := \
	third_party/linux/include/rnnoise.h \
	third_party/linux/lib/librnnoise.a \
	third_party/linux/lib/librnnoise.la \
	third_party/linux/lib/pkgconfig/rnnoise.pc

THIRD_PARTY_LINUX_PORTAUDIO_FILES := \
	third_party/linux/include/pa_linux_pulseaudio.h \
	third_party/linux/include/portaudio.h \
	third_party/linux/lib/libportaudio.a \
	third_party/linux/lib/libportaudio.la \
	third_party/linux/lib/pkgconfig/portaudio-2.0.pc

THIRD_PARTY_LINUX_LAME_FILES := \
	third_party/linux/include/lame/lame.h \
	third_party/linux/lib/libmp3lame.a \
	third_party/linux/lib/libmp3lame.la

THIRD_PARTY_LINUX_FFMPEG_FILES := \
	internal/ffmpeg/bin/ffmpeg.gz

third_party/linux: \
	$(THIRD_PARTY_LINUX_RNNOISE_FILES) \
	$(THIRD_PARTY_LINUX_PORTAUDIO_FILES) \
	$(THIRD_PARTY_LINUX_FFMPEG_FILES)

$(THIRD_PARTY_LINUX_RNNOISE_FILES): deps_build/rnnoise
	mkdir -p third_party/linux

	cd deps_build/rnnoise && \
	make distclean; \
	./configure \
		--disable-shared --enable-static \
		--disable-doc --disable-examples \
		--enable-x86-rtcd \
		--prefix=${CURDIR}/third_party/linux && \
	make install

	go clean -cache

$(THIRD_PARTY_LINUX_PORTAUDIO_FILES): deps_build/portaudio
	mkdir -p third_party/linux

	cd deps_build/portaudio && \
	make distclean; \
	./configure \
		--disable-shared --enable-static \
		--without-audioio --without-sndio --without-jack --without-oss --without-asihpi --without-winapi --without-alsa \
		--with-pulseaudio \
		--prefix=${CURDIR}/third_party/linux && \
	make install

	go clean -cache

$(THIRD_PARTY_LINUX_LAME_FILES):
	mkdir -p third_party/linux

	mkdir -p /tmp/liblame-docs
	
	cd deps_build/lame && \
	make clean; \
	./configure \
		--enable-static --disable-shared \
		--disable-dependency-tracking \
		--disable-gtktest --disable-frontend \
		--docdir=/tmp/liblame-docs --mandir=/tmp/liblame-docs \
		--prefix=${CURDIR}/third_party/linux && \
	make install

$(THIRD_PARTY_LINUX_FFMPEG_FILES): $(THIRD_PARTY_LINUX_LAME_FILES) deps_build/ffmpeg
	cd deps_build/ffmpeg && \
	make distclean; \
	./configure \
		$(FFMPEG_COMMON_BUILD_FLAGS) \
		--extra-cflags="-I${CURDIR}/third_party/linux/include" \
		--extra-ldflags="-L${CURDIR}/third_party/linux/lib" && \
	make

	mkdir -p internal/ffmpeg/bin
	gzip -9 --stdout --keep deps_build/ffmpeg/ffmpeg > internal/ffmpeg/bin/ffmpeg.gz

THIRD_PARTY_WINDOWS_RNNOISE_FILES := \
	third_party/windows/include/rnnoise.h \
	third_party/windows/lib/librnnoise.a \
	third_party/windows/lib/librnnoise.la \
	third_party/windows/lib/pkgconfig/rnnoise.pc

THIRD_PARTY_WINDOWS_PORTAUDIO_FILES := \
	third_party/windows/include/pa_win_waveformat.h \
	third_party/windows/include/pa_win_wmme.h \
	third_party/windows/include/portaudio.h \
	third_party/windows/lib/libportaudio.a \
	third_party/windows/lib/libportaudio.la \
	third_party/windows/lib/pkgconfig/portaudio-2.0.pc

THIRD_PARTY_WINDOWS_LAME_FILES := \
	third_party/windows/include/lame/lame.h \
	third_party/windows/lib/libmp3lame.a \
	third_party/windows/lib/libmp3lame.la

THIRD_PARTY_WINDOWS_FFMPEG_FILES := \
	internal/ffmpeg/bin/ffmpeg.exe.gz

third_party/windows: \
	rsrc_windows_amd64.syso \
	$(THIRD_PARTY_WINDOWS_RNNOISE_FILES) \
	$(THIRD_PARTY_WINDOWS_PORTAUDIO_FILES) \
	$(THIRD_PARTY_WINDOWS_FFMPEG_FILES)

tools/rsrc:
	GOBIN=${CURDIR}/tools go install github.com/akavel/rsrc@latest

rsrc_windows_amd64.syso: tools/rsrc
	# convert assets/icons/soundpad.png -colors 256 assets/soundpad.ico
	tools/rsrc -ico assets/soundpad.ico -arch amd64 -o rsrc_windows_amd64.syso

$(THIRD_PARTY_WINDOWS_RNNOISE_FILES):
	mkdir -p third_party/windows

	cd deps_build/rnnoise && \
	make distclean; \
	./configure \
        --disable-shared --enable-static \
        --disable-doc --disable-examples \
        --host=x86_64-w64-mingw32 \
        --enable-x86-rtcd \
        --prefix=${CURDIR}/third_party/windows && \
	make install

	go clean -cache

$(THIRD_PARTY_WINDOWS_PORTAUDIO_FILES):
	mkdir -p third_party/windows

	cd deps_build/portaudio && \
	make distclean; \
	./configure \
		--disable-shared --enable-static \
        --without-audioio --without-sndio --without-jack --without-oss --without-asihpi --without-alsa --without-pulseaudio \
		--with-winapi=wmme \
        --host=x86_64-w64-mingw32 \
		--prefix=${CURDIR}/third_party/windows && \
	make install

	go clean -cache

$(THIRD_PARTY_WINDOWS_LAME_FILES):
	mkdir -p third_party/windows

	mkdir -p /tmp/liblame-docs
	
	cd deps_build/lame && \
	make clean; \
	./configure \
		--host=x86_64-w64-mingw32 \
		--enable-static --disable-shared \
		--disable-dependency-tracking \
		--disable-gtktest --disable-frontend \
		--docdir=/tmp/liblame-docs --mandir=/tmp/liblame-docs \
		--prefix=${CURDIR}/third_party/windows && \
	make install

$(THIRD_PARTY_WINDOWS_FFMPEG_FILES): $(THIRD_PARTY_WINDOWS_LAME_FILES) deps_build/ffmpeg
	cd deps_build/ffmpeg && \
	make distclean; \
	./configure \
		$(FFMPEG_COMMON_BUILD_FLAGS) \
		--cross-prefix=x86_64-w64-mingw32- \
		--enable-cross-compile \
		--target-os=mingw32 \
		--arch=x86_64 \
		--extra-cflags="-I${CURDIR}/third_party/windows/include" \
		--extra-ldflags="-L${CURDIR}/third_party/windows/lib" && \
	make

	mkdir -p internal/ffmpeg/bin
	gzip -9 --stdout --keep deps_build/ffmpeg/ffmpeg.exe > internal/ffmpeg/bin/ffmpeg.exe.gz
