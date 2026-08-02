module.exports = {
  apps: [
    {
      name: "xlwms-api-manager",
      cwd: "/home/ubuntu/xlwms-api-manager",
      script: "./scripts/run-server.sh",
      interpreter: "none",
      autorestart: true,
      max_restarts: 10,
      restart_delay: 2000,
      time: true
    },
    {
      name: "shein-go-manager",
      cwd: "/home/ubuntu/xlwms-api-manager",
      script: "./scripts/run-shein-server.sh",
      interpreter: "none",
      autorestart: true,
      max_restarts: 10,
      restart_delay: 2000,
      time: true
    }
  ]
};
