# dojo typed-judging driver (embedded in the binary, written to tmp at run
# time). Usage: python3 typed_driver.py <solution.py> <method>
# stdin: JSON array of arguments. stdout: JSON-encoded return value.
import importlib.util
import json
import sys

src, method = sys.argv[1], sys.argv[2]
spec = importlib.util.spec_from_file_location("solution", src)
mod = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mod)

args = json.loads(sys.stdin.read())
result = getattr(mod.Solution(), method)(*args)
print(json.dumps(result))
