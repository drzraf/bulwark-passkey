#!/bin/bash

[[ -n $DEBUG && $DEBUG != 0 ]] && DEBUG=-debug || DEBUG=

set -e

cd frontend
mkdir -p src/proto || true
npm run build
cd ..

cp -r frontend/build app/frontend_dist
cd app
~/go/bin/wails build -s -skipbindings $DEBUG
cd ..
mkdir -p output
mv app/build/bin/bulwark_passkey output/
