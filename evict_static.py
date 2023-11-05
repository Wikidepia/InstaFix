import os
from glob import glob

# Maximum static directory size
STATIC_MAX_SIZE = 25_000_000_000

# Get files in static directory, get least recently accessed first
files = sorted(glob("static/*.*"), key=os.path.getatime, reverse=True)
print(files)
# file_sizes = [os.path.getsize(f) for f in files]

# # Delete files until directory size is below maximum
# while sum(file_sizes) > STATIC_MAX_SIZE:
#     print("Deleting", files[0])
#     os.remove(files.pop(0))
#     file_sizes.pop(0)
