# syntax=docker/dockerfile:1
FROM node:22-alpine
WORKDIR /app
COPY package*.json tsconfig.json ./
RUN npm install --no-audit --no-fund
COPY src ./src
RUN npx tsc
USER node
CMD ["node", "dist/compact.js"]
