# This directory holds the Apple Music wrapper files for Docker builds.
#
# To enable Apple Music DRM decryption in Docker:
#
# 1. Download the wrapper release:
#    wget https://github.com/WorldObservationLog/wrapper/releases/download/wrapper.x86_64.latest/Wrapper.x86_64.latest.zip
#
# 2. Extract into this directory:
#    unzip Wrapper.x86_64.latest.zip -d docker/wrapper/
#
# 3. Build the full Docker image:
#    docker build --target full -t musicbot-go:full .
#
# 4. Run with Apple ID credentials (first time):
#    docker run -e APPLE_MUSIC_USERNAME=you@icloud.com -e APPLE_MUSIC_PASSWORD=yourpass ...
#
# After first login, the account session is persisted in docker-data/wrapper-data/
# and subsequent runs don't need the credentials.
#
# The wrapper binary and rootfs are NOT committed to git.
