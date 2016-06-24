FROM nginx:stable-alpine
MAINTAINER FeelGo
VOLUME /data
COPY nginx.conf /etc/nginx/nginx.conf
COPY htpasswd /usr/share/nginx/html