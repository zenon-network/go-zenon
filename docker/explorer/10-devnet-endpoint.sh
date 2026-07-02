#!/bin/sh
set -eu

endpoint="${EXPLORER_DEFAULT_ENDPOINT:-http://localhost:35997}"

cat > /usr/share/nginx/html/devnet-endpoint.js <<EOF
(function () {
  var endpoint = "${endpoint}";
  try {
    var nodes = JSON.parse(localStorage.getItem("nodes") || "[]");
    if (!Array.isArray(nodes)) {
      nodes = [];
    }
    nodes = nodes.filter(function (node) {
      return node && node !== endpoint;
    });
    nodes.unshift(endpoint);
    localStorage.setItem("nodes", JSON.stringify(nodes));
    localStorage.setItem("defaultEndpoint", endpoint);
    window.__ZENON_DEVNET_ENDPOINT__ = endpoint;
  } catch (err) {
    console.warn("Unable to configure devnet explorer endpoint", err);
  }
})();
EOF
