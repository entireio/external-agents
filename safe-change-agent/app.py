"""Command-line entry point for the Safe Change Agent scaffold."""

from services.code_analyzer import CodeAnalyzer


def main() -> None:
    """Provide a minimal confirmation that the project is configured."""
    analyzer = CodeAnalyzer()
    print(f"Safe Change Agent is ready. Analyzer root: {analyzer.root}")


if __name__ == "__main__":
    main()
