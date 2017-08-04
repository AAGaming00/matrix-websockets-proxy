# matrix-websockets-proxy

This project provides a websockets wrapper for a Matrix.org homeserver. See
https://github.com/matrix-org/matrix-doc/blob/master/drafts/websockets.rst
for information on the protocol it implements.

To run it, you will need a working `go` installation, including a correctly-set
[GOPATH](https://golang.org/doc/code.html#GOPATH). 

You can then download and build matrix-websockets-proxy with:

    go get github.com/krombel/matrix-websockets-proxy

The above will clone the repository into
`$GOPATH/src/github.com/krombel/matrix-websockets-proxy`, and build the
binary at `$GOPATH/bin/matrix-websockets`.

To run it, just do:

    $GOPATH/bin/matrix-websockets-proxy

To rebuild, use:

    go build github.com/krombel/matrix-websockets-proxy
    
To update to the latest state, run:

    go get -u github.com/krombel/matrix-websockets-proxy

### integration with riot-web (in particular matrix-js-sdk)
Currently there exists one reference implementation to use this proxy. Use my fork of [matrix-js-sdk](https://github.com/krombel/matrix-js-sdk/tree/krombel_websockets) and compile riot-web on your own or use my hosted instance at
[https://msgs.tk/riotws](https://msgs.tk/riotws)

### nginx-integration
To make this work you can integrate this proxy using nginx as follows:
```
        location /_matrix/client/ws/r0 {
                proxy_pass http://127.0.0.1:8009/stream;
                proxy_http_version 1.1;
                proxy_set_header Upgrade $http_upgrade;
                proxy_set_header Connection "upgrade";
                proxy_set_header Origin "";
        }
```


