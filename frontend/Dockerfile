# Build Stage — cache bust: 2026-03-31b
FROM node:18-alpine AS builder
WORKDIR /app
COPY package.json package-lock.json* ./
RUN npm install
COPY . .
ARG VITE_APP_MODULE=apuracao
ENV VITE_APP_MODULE=$VITE_APP_MODULE
RUN npm run build

# Serve Stage
FROM nginx:stable-alpine
COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]