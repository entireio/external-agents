
"""
Module to expose more detailed version info for the installed `numpy`
"""
version = "2.5.2"
__version__ = version
full_version = version

git_revision = "48fecee5453aa1d31e6b79dcb3969dc1a6d1a891"
release = 'dev' not in version and '+' not in version
short_version = version.split("+")[0]
