import { serve } from "bun";

const server = serve({
  port: 3000,
  async fetch(req) {
    const url = new URL(req.url);

    // Handle POST request to /api endpoint
    if (req.method === "POST" && url.pathname === "/api") {
      try {
        // Parse JSON body
        const body = await req.json();

        // Log the request to console
        // biome-ignore lint/suspicious/noConsoleLog: <explanation>
        console.log("Received POST request:", {
          timestamp: new Date().toISOString(),
          path: url.pathname,
          body,
        });

        // Return success response
        return new Response(
          JSON.stringify({
            status: "success",
            message: "Data received successfully",
          }),
          {
            headers: { "Content-Type": "application/json" },
          }
        );
      } catch (error) {
        console.error("Error processing request:", error);
        return new Response(
          JSON.stringify({
            status: "error",
            message: "Failed to process request",
          }),
          {
            status: 400,
            headers: { "Content-Type": "application/json" },
          }
        );
      }
    }

    // Handle 404 for any other routes
    return new Response("Not Found", { status: 404 });
  },
});

// biome-ignore lint/suspicious/noConsoleLog: <explanation>
console.log(`Server running at http://localhost:${server.port}`);
