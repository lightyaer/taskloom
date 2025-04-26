#!/bin/bash

cat > .env << EOL
PORT=8080
TURSO_URL=file:taskloom.db
TURSO_TOKEN=
JWT_SECRET=your-secret-key-please-change-me
EOL

echo ".env file created successfully!"

cat > .env.example << EOL
PORT=8080
TURSO_URL=file:taskloom.db
TURSO_TOKEN=
JWT_SECRET=your-secret-key-please-change-me
EOL

echo ".env.example file created successfully!"

chmod +x setup_env.sh 