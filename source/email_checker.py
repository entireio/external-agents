"""Validate an email address entered at the command line."""

import re


EMAIL_PATTERN = re.compile(r"^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$")


def is_valid_email(email: str) -> bool:
    """Return True when *email* has a basic valid email format."""
    return bool(EMAIL_PATTERN.fullmatch(email.strip()))


def main() -> None:
    email = input("Enter an email address: ")
    if is_valid_email(email):
        print("Valid email address.")
    else:
        print("Invalid email address.")


if __name__ == "__main__":
    main()
