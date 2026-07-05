# Build the SPA, then serve the static dist with nginx.
FROM node:24-alpine AS build
WORKDIR /app

# Vite build metadata (compile-time).
ARG VITE_APP_VERSION=0.1.0
ARG VITE_GIT_COMMIT=none
ARG VITE_BUILD_TIME=unknown
ENV VITE_APP_VERSION=$VITE_APP_VERSION \
    VITE_GIT_COMMIT=$VITE_GIT_COMMIT \
    VITE_BUILD_TIME=$VITE_BUILD_TIME

COPY package.json package-lock.json* ./
RUN npm ci
COPY . .
RUN npm run build

FROM nginx:1.27-alpine
COPY --from=build /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY nginx-security-headers.conf /etc/nginx/conf.d/security-headers.conf
EXPOSE 80
