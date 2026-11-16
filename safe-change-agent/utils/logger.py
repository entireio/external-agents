"""Logging configuration for the application."""

import logging


def get_logger(name: str) -> logging.Logger:
    """Return a named logger without imposing global logging configuration."""
    return logging.getLogger(name)
