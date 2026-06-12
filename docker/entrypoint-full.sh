#!/bin/sh
set -e

# --- Apple Music wrapper (optional) ---
# If the wrapper binary exists and Apple Music credentials are provided,
# start the wrapper in the background for DRM decryption.
WRAPPER_BIN="/app/wrapper/wrapper"
WRAPPER_ROOTFS="/app/wrapper/rootfs"
WRAPPER_DATA="/app/workdir/wrapper-data"

if [ -x "$WRAPPER_BIN" ] && [ -d "$WRAPPER_ROOTFS" ]; then
    # Ensure persistent data dir exists
    mkdir -p "$WRAPPER_DATA"

    # Link data into rootfs if not already done
    ROOTFS_DATA="$WRAPPER_ROOTFS/data"
    if [ ! -d "$ROOTFS_DATA" ] || [ -z "$(ls -A "$ROOTFS_DATA" 2>/dev/null)" ]; then
        mkdir -p "$ROOTFS_DATA"
    fi
    # Use bind mount or symlink for persistent wrapper account data
    if mountpoint -q "$ROOTFS_DATA" 2>/dev/null; then
        : # already mounted
    else
        cp -a "$WRAPPER_DATA/." "$ROOTFS_DATA/" 2>/dev/null || true
    fi

    TOKEN_DB="$ROOTFS_DATA/data/com.apple.android.music/files/mpl_db/kvs.sqlitedb"
    WRAPPER_ARGS="-H 127.0.0.1"

    if [ ! -f "$TOKEN_DB" ]; then
        if [ -n "$APPLE_MUSIC_USERNAME" ] && [ -n "$APPLE_MUSIC_PASSWORD" ]; then
            echo "[wrapper] First login: starting wrapper with credentials..."
            "$WRAPPER_BIN" -L "${APPLE_MUSIC_USERNAME}:${APPLE_MUSIC_PASSWORD}" -F $WRAPPER_ARGS &
        else
            echo "[wrapper] No account database and no credentials provided. Wrapper disabled."
            echo "[wrapper] Set APPLE_MUSIC_USERNAME and APPLE_MUSIC_PASSWORD to enable."
        fi
    else
        echo "[wrapper] Starting wrapper with existing account..."
        "$WRAPPER_BIN" $WRAPPER_ARGS &
    fi

    WRAPPER_PID=$!
    # Wait a moment for wrapper to start
    sleep 2
    if kill -0 $WRAPPER_PID 2>/dev/null; then
        echo "[wrapper] Running (PID $WRAPPER_PID) on ports 10020/20020/30020"
    else
        echo "[wrapper] Failed to start, Apple Music DRM decryption unavailable."
    fi
fi

# --- Start MusicBot-Go ---
exec /app/MusicBot-Go "$@"
