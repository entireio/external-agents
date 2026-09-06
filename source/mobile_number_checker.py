"""Validate a mobile number entered at the command line."""

import re


# Accepts an optional + country code, spaces, hyphens, or parentheses.
MOBILE_PATTERN = re.compile(r"^\+?[0-9][0-9\s()-]{7,}[0-9]$")


def is_valid_mobile_number(number: str) -> bool:
    """Return True when *number* contains 8 to 15 digits in a mobile format."""
    cleaned = number.strip()
    digits = re.sub(r"\D", "", cleaned)
    return bool(MOBILE_PATTERN.fullmatch(cleaned)) and 8 <= len(digits) <= 15


def main() -> None:
    number = input("Enter a mobile number: ")
    if is_valid_mobile_number(number):
        print("Valid mobile number.")
    else:
        print("Invalid mobile number.")


if __name__ == "__main__":
    main()
