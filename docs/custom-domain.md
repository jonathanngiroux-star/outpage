# Custom domain, TLS, and reverse proxy

Outpage serves plain HTTP on the address you pass it. It does not terminate
TLS as a product and never will — that job belongs to the reverse proxy or
load balancer you already run.

Example with Caddy (automatic TLS):

```
status.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

Example with nginx:

```
server {
    listen 443 ssl;
    server_name status.example.com;
    ssl_certificate     /etc/letsencrypt/live/status.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/status.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }
}
```

Points to keep in mind:

- Bind `outpage serve --http 127.0.0.1:8080` to loopback and let the proxy own
  the public interface.
- `/` and `/donate` are the only public paths. There is no admin HTTP surface.
- Set `X-Forwarded-For` if you log; Outpage does not log subscriber IPs.
