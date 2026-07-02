FROM alpine:3.20 AS explorer

ARG EXPLORER_COMMIT=84b772981f0dd25ed52758f6244f9e1f8d54634b

RUN apk add --no-cache ca-certificates wget

WORKDIR /explorer

RUN set -eux; \
    base="https://raw.githubusercontent.com/zenon-network/explorer.zenon.network/${EXPLORER_COMMIT}"; \
    mkdir -p assets; \
    wget -O 3rdpartylicenses.txt "${base}/3rdpartylicenses.txt"; \
    wget -O 404.html "${base}/404.html"; \
    wget -O CNAME "${base}/CNAME"; \
    wget -O favicon.ico "${base}/favicon.ico"; \
    wget -O index.html "${base}/index.html"; \
    wget -O main.4f7c3c3f8f54430c53d8.js "${base}/main.4f7c3c3f8f54430c53d8.js"; \
    wget -O polyfills.1b391f26029e71638c65.js "${base}/polyfills.1b391f26029e71638c65.js"; \
    wget -O runtime.7b63b9fd40098a2e8207.js "${base}/runtime.7b63b9fd40098a2e8207.js"; \
    wget -O styles.3bbcf2e5d59b09fa4d73.css "${base}/styles.3bbcf2e5d59b09fa4d73.css"; \
    wget -O assets/mdi.svg "${base}/assets/mdi.svg"; \
    sed -i 's#</head>#  <script src="devnet-endpoint.js"></script>\n</head>#' index.html

FROM nginx:1.27-alpine

COPY --from=explorer /explorer /usr/share/nginx/html
COPY docker/explorer/nginx.conf /etc/nginx/conf.d/default.conf
COPY docker/explorer/10-devnet-endpoint.sh /docker-entrypoint.d/10-devnet-endpoint.sh

RUN chmod +x /docker-entrypoint.d/10-devnet-endpoint.sh
