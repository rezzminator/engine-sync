// Package pluginlog writes the plugin's errors, warnings and info rows as
// bounded JSON lines. Logging failure never changes a command's work result;
// stderr receives one notice per failing streak, re-armed by a successful write.
package pluginlog
