// Package config holds the one fleet constant the OpenCode compiler shares
// with pfm: the name of the MCP server every client registers.
package config

// MCPServerProfessor is the one MCP server every client registers: a Claude
// tool named mcp__professor__* resolves to it.
const MCPServerProfessor = "professor"
