FROM nginx:stable-alpine
MAINTAINER FeelGo
COPY nginx.conf /etc/nginx/nginx.conf
COPY htpasswd /usr/share/nginx/html