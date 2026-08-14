import { request } from "../client.js";
import { register, z } from "./register.js";

const commandSchema = { subcommand: z.string().min(1), args: z.array(z.string()).optional(), timeout_sec: z.number().int().positive().optional(), cwd: z.string().optional(), env: z.record(z.string(), z.string()).optional(), stdin_base64: z.string().optional(), async: z.boolean().optional(), dry_run: z.boolean().optional() };
export function registerDockerTools(server) {
  register(server, "nas_docker_info", "Read Container Station/Docker engine information.", {}, () => request("GET", "/v1/docker/info"), { readOnlyHint: true });
  register(server, "nas_docker_containers", "List all Docker containers.", {}, () => request("GET", "/v1/docker/containers"), { readOnlyHint: true });
  register(server, "nas_docker_images", "List Docker images.", {}, () => request("GET", "/v1/docker/images"), { readOnlyHint: true });
  register(server, "nas_docker_command", "Run a Docker CLI subcommand. In full_trust, destructive actions execute directly and remain audited.", commandSchema, (args) => request("POST", "/v1/docker/command", args), { destructiveHint: true });
  for (const [name, subcommand, description] of [["nas_docker_inspect", "inspect", "Inspect a container or image."], ["nas_docker_logs", "logs", "Read container logs."], ["nas_docker_action", "container", "Perform a container action."], ["nas_docker_run", "run", "Create and start a Docker container."], ["nas_docker_create", "create", "Create a Docker container."], ["nas_docker_remove", "rm", "Remove Docker containers."], ["nas_docker_exec", "exec", "Run a command in a container."], ["nas_docker_pull", "pull", "Pull a Docker image."], ["nas_docker_image_remove", "rmi", "Remove a Docker image."], ["nas_docker_network", "network", "Manage Docker networks."], ["nas_docker_volume", "volume", "Manage Docker volumes."], ["nas_docker_compose", "compose", "Run Docker Compose."]]) {
    register(server, name, description, { args: z.array(z.string()).default([]), timeout_sec: z.number().int().positive().optional(), cwd: z.string().optional(), env: z.record(z.string(), z.string()).optional(), stdin_base64: z.string().optional(), async: z.boolean().optional(), dry_run: z.boolean().optional(), name: z.string().optional(), action: z.string().optional(), tail: z.number().int().positive().optional() }, (args) => {
      let commandArgs = args.args || [];
      if (name === "nas_docker_inspect") commandArgs = [args.name, ...commandArgs];
      if (name === "nas_docker_logs") commandArgs = ["--tail", String(args.tail || 200), args.name, ...commandArgs];
      if (name === "nas_docker_action") commandArgs = [args.action, args.name, ...commandArgs];
      return request("POST", "/v1/docker/command", { subcommand, args: commandArgs, timeout_sec: args.timeout_sec, cwd: args.cwd, env: args.env, stdin_base64: args.stdin_base64, async: args.async, dry_run: args.dry_run });
    }, { destructiveHint: !["nas_docker_inspect", "nas_docker_logs", "nas_docker_exec", "nas_docker_pull"].includes(name) });
  }
  register(server, "nas_docker_stats", "Read one-shot Docker stats.", { args: z.array(z.string()).optional(), timeout_sec: z.number().int().positive().optional() }, (args) => request("POST", "/v1/docker/command", { subcommand: "stats", args: args.args?.length ? args.args : ["--no-stream"], timeout_sec: args.timeout_sec }), { readOnlyHint: true });
  register(server, "nas_docker_reconstruct", "Inspect a container and produce a reviewable equivalent docker run command and compose-oriented fields.", { name: z.string().min(1) }, async ({ name }) => {
    const response = await request("POST", "/v1/docker/command", { subcommand: "inspect", args: [name] });
    const item = JSON.parse(response.stdout || "[]")[0] || {};
    const config = item.Config || {};
    const host = item.HostConfig || {};
    const command = ["docker", "run"];
    if (host.RestartPolicy?.Name) command.push("--restart", host.RestartPolicy.Name);
    for (const env of config.Env || []) command.push("-e", env);
    for (const bind of host.Binds || []) command.push("-v", bind);
    for (const [containerPort, bindings] of Object.entries(host.PortBindings || {})) for (const binding of bindings || []) command.push("-p", `${binding.HostIp || ""}${binding.HostIp ? ":" : ""}${binding.HostPort}:${containerPort}`);
    if (config.Image) command.push(config.Image);
    command.push(...(config.Cmd || []));
    return { inspect: item, docker_run: command, compose: { image: config.Image, environment: config.Env, volumes: host.Binds, restart: host.RestartPolicy?.Name, ports: host.PortBindings, network_mode: host.NetworkMode } };
  }, { readOnlyHint: true });
}
