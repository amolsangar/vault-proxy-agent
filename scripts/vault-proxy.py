import logging
import time
import requests
import hashlib

from typing import Dict, Callable, Tuple
from dataclasses import dataclass
from flask import Flask, request, Response

log = logging.getLogger(__name__)
app = Flask(__name__)

"""
This is a reference implementation of vault-proxy in python that shouldn't be used. It was a POC that has now been migrated
to the Golang package.
"""

@dataclass
class Config:
    VAULT_PORT = 8200
    PROXY_PORT = 8199
    DEFAULT_EXPIRATION = 60  # expiration time in seconds
    PURGE_FREQUENCY = 60 * 10  # purge cache every 10 minutes


@dataclass
class CacheableRequest:
    expires: float
    response: Response

    def is_expired(self) -> bool:
        return time.time() > self.expires

    @staticmethod
    def build(response: Response):
        return CacheableRequest(expires=time.time() + Config.DEFAULT_EXPIRATION, response=response)


GLOBAL_CACHE: Dict[str, CacheableRequest] = {}
LAST_CACHE_PURGE: float = 0


def purge_old_cache_entries() -> None:
    """
    Purges old cache entries to avoid memory creep
    """
    global LAST_CACHE_PURGE

    if LAST_CACHE_PURGE < time.time() - Config.PURGE_FREQUENCY:
        app.logger.info("PURGING CACHE.")
        for cache_key, value in GLOBAL_CACHE.items():
            if value.is_expired():
                del GLOBAL_CACHE[cache_key]

        LAST_CACHE_PURGE = time.time()
    else:
        app.logger.info(f"Not purging cache...")


def get_from_cache_or_refresh(request, refresher: Callable) -> Response:
    """
    Retrieves the result of a previously cached request result from the global cache or executes the refresher() method
    and stores its result in the global cache.

    ":param request: A hydrated flask request object.
    :param refresher: Method whose result will be cached for future calls to this method. Must return a 'Response'
    :return: Valid requests module HTTP Response object
    """

    purge_old_cache_entries()

    token, namespace, path = parse_vault_request(request)
    app.logger.info(f"Got Token: {token} and path: {path}")
    request_hash = hashlib.md5(f'{token}-{namespace}-{path}'.encode()).hexdigest()

    # Update cache if necessary
    if request_hash not in GLOBAL_CACHE \
            or (request_hash in GLOBAL_CACHE
                and GLOBAL_CACHE[request_hash].is_expired()):

        app.logger.info(f"KV is missing from cache or is expired.")
        GLOBAL_CACHE[request_hash] = CacheableRequest.build(refresher())
    else:
        app.logger.info(f"Found kv: {path} in cache, returning from cache.")

    return GLOBAL_CACHE[request_hash].response


def parse_vault_request(request) -> Tuple[str, str, str]:
    """
    Parses out the Vault auth token & path
    :param request: hydrated flask request object
    :return: Tuple[VaultToken, Vault Namespace, Vault Path]
    """
    return request.headers.get('X-Vault-Token', None), request.headers.get('X-Vault-Namespace', None), request.path


def proxy_request(request) -> Response:
    """
    Proxies a flask request and returns the requests.Response result.
    :param request: a hydrated flask request object
    :return: Response: A hydrated python.requests module Response object.
    """
    resp = requests.request(
        method=request.method,
        url=request.url.replace(f'{Config.PROXY_PORT}', f'{Config.VAULT_PORT}'),
        headers={key: value for (key, value) in request.headers if key != 'Host'},
        data=request.get_data(),
        cookies=request.cookies,
        allow_redirects=False)

    excluded_headers = ['content-encoding', 'content-length', 'transfer-encoding', 'connection']
    headers = [(name, value) for (name, value) in resp.raw.headers.items()
               if name.lower() not in excluded_headers]

    return Response(resp.content, resp.status_code, headers)


@app.route('/', defaults={'path': ''})
@app.route('/<path:path>')
def catch_all(path):
    # Insert arbitrary request-type filtering logic here -- in this instance, only cache GET requests for k/v pairs.
    token, namespace, path = parse_vault_request(request)
    if request.method == 'GET' and "v1/kv/" in path and token:
        return get_from_cache_or_refresh(request, lambda: proxy_request(request))

    return proxy_request(request)


@app.route("/z/ping")
def ok():
    return "Ok!"


if __name__ == "__main__":
    log.info(f"Starting app on http://localhost:{Config.PROXY_PORT}")
    app.run(debug=True, host='0.0.0.0', port=Config.PROXY_PORT, threaded=True)

