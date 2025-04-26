# TaskLoom

TaskLoom is a dynamic HTTP scheduler built in Go. It allows you to schedule HTTP POST requests to be executed at specified times using cron expressions.

## Features

- Create projects to group related tasks
- Define schedules using cron expressions
- Configure tasks with custom headers and body
- User authentication with OAuth or magic email links
- Rate limiting based on project settings
- Retry failed requests
- Execution logs for all task runs

## Installation

### Prerequisites

- Go 1.20 or higher
- Turso database (or use SQLite locally)

### Setup

1. Clone the repository:
   ```
   git clone https://github.com/lightyaer/taskloom.git
   cd taskloom
   ```

2. Copy the example environment file and configure it:
   ```
   cp .env.example .env
   ```

3. Edit the `.env` file with your configuration:
   ```
   PORT=8080
   TURSO_URL=file:taskloom.db  # Local SQLite, or use your Turso URL
   TURSO_TOKEN=                # Leave empty for local SQLite, or use your Turso token
   JWT_SECRET=your-secret-key-please-change-me
   ```

4. Build the application:
   ```
   go build -o taskloom ./cmd/server
   ```

5. Run the application:
   ```
   ./taskloom
   ```

## API Endpoints

### Authentication

- `POST /auth/email/login` - Request a magic login link
- `GET /auth/email/verify?token={token}` - Verify email and get JWT token
- `POST /auth/oauth/login` - Login with OAuth provider

### Projects

- `GET /api/projects` - List all projects
- `POST /api/projects` - Create a new project
- `GET /api/projects/{id}` - Get a project by ID
- `PUT /api/projects/{id}` - Update a project
- `DELETE /api/projects/{id}` - Delete a project

### Schedules

- `GET /api/schedules?project_id={projectId}` - List all schedules for a project
- `POST /api/schedules` - Create a new schedule
- `GET /api/schedules/{id}` - Get a schedule by ID
- `PUT /api/schedules/{id}` - Update a schedule
- `DELETE /api/schedules/{id}` - Delete a schedule

### Tasks

- `GET /api/tasks?schedule_id={scheduleId}` - List all tasks for a schedule
- `POST /api/tasks` - Create a new task
- `GET /api/tasks/{id}` - Get a task by ID
- `PUT /api/tasks/{id}` - Update a task
- `DELETE /api/tasks/{id}` - Delete a task

### Execution Logs

- `GET /api/logs/task/{taskId}` - List execution logs for a task
- `GET /api/logs/schedule/{scheduleId}` - List execution logs for a schedule

## Project Structure

```
taskloom/
├─ cmd/
│  └─ server/           # Main server entry point
├─ pkg/
│  ├─ auth/             # Authentication services
│  ├─ database/         # Database access layer
│  ├─ handlers/         # HTTP handlers
│  ├─ models/           # Data models
│  └─ scheduler/        # Task scheduling engine
├─ internal/
│  └─ utils/            # Utility functions
├─ .env                 # Environment variables
└─ README.md            # This file
```

## Scheduled Task Execution

Tasks are executed according to their schedules defined by cron expressions. Each task makes a POST request to the specified URL with the configured headers and body.

Rate limiting is applied at the project level, and failed requests can be automatically retried based on project settings.

## Deployment

### Docker Deployment

You can deploy TaskLoom using Docker with the included Dockerfile:

```bash
# Build the Docker image
docker build -t taskloom .

# Run the container with local SQLite
docker run -p 8080:8080 \
  -e JWT_SECRET=your-secret-key \
  -v taskloom_data:/data \
  taskloom

# OR with Turso Cloud
docker run -p 8080:8080 \
  -e JWT_SECRET=your-secret-key \
  -e TURSO_URL=https://your-database.turso.io \
  -e TURSO_TOKEN=your-turso-auth-token \
  taskloom
```

Alternatively, use docker-compose:

```bash
# Using default configuration (local SQLite)
docker-compose up

# OR with environment variables for Turso Cloud
TURSO_URL=https://your-database.turso.io TURSO_TOKEN=your-turso-auth-token docker-compose up
```

### Deploying to Render.com

TaskLoom can be easily deployed to Render.com:

1. Push your code to a Git repository (GitHub, GitLab, etc.)
2. Log in to your Render.com account
3. Click "New" and select "Blueprint"
4. Connect to your Git repository
5. Render will detect the `render.yaml` file and configure the service
6. Modify the environment variables if needed (e.g., to use Turso Cloud)
7. Click "Apply"

The deployment will automatically:
- Build the Docker image
- Set up the required environment variables
- Create a persistent disk for the database
- Deploy the application with health checks

#### Manual Deployment

Alternatively, you can deploy manually:

1. Log in to Render.com
2. Select "New Web Service"
3. Choose "Docker" as the environment
4. Connect to your repository
5. Set the following:
   - Name: taskloom
   - Environment: Docker
   - Region: Singapore (closest to India)
   - Branch: main (or your preferred branch)
   - Plan: Starter
6. Add environment variables:
   - PORT: 8080
   - JWT_SECRET: (generate a secure key)
   - TURSO_URL: file:/data/taskloom.db (or your Turso Cloud URL)
   - TURSO_TOKEN: (leave empty for local SQLite, or add your Turso auth token)
7. Add a disk (only needed for local SQLite):
   - Name: data
   - Mount Path: /data
   - Size: 1 GB

### Database Options

#### Local SQLite (Default)

By default, TaskLoom uses a local SQLite database stored in the `/data` directory. This is suitable for:
- Single-instance deployments
- Development and testing
- Simple production setups

#### Turso Database (Recommended for Production)

For production deployments, especially with multiple instances, it's recommended to use Turso Cloud:

1. Sign up for a Turso account at https://turso.tech
2. Create a database
3. Get your database URL and authentication token
4. Set the following environment variables:
   - TURSO_URL: Your Turso database URL
   - TURSO_TOKEN: Your Turso authentication token

### SQLite Performance

TaskLoom is configured to use SQLite in WAL (Write-Ahead Logging) mode for improved concurrency and performance. This allows multiple read operations to occur simultaneously with write operations, reducing database locking issues.

## License

MIT 