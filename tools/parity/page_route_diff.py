import argparse
import requests
import json
import sys


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--py-base", required=True)
    parser.add_argument("--go-base", required=True)
    parser.add_argument("--out", required=True)
    args = parser.parse_args()

    # We want to test different routes
    routes = [
        "/",
        "/login",
        "/imagine",
        "/voice",
        "/video",
        "/chat",
        "/admin",
        "/admin/login",
        "/admin/config",
        "/admin/cache",
        "/admin/token",
        "/static/public/pages/login.html",
    ]

    results = []
    has_diff = False

    for route in routes:
        py_url = args.py_base + route
        go_url = args.go_base + route

        try:
            py_resp = requests.get(py_url, allow_redirects=False)
            py_status = py_resp.status_code
            py_location = py_resp.headers.get("Location", "")
        except Exception as e:
            py_status = 0
            py_location = str(e)

        try:
            go_resp = requests.get(go_url, allow_redirects=False)
            go_status = go_resp.status_code
            go_location = go_resp.headers.get("Location", "")
        except Exception as e:
            go_status = 0
            go_location = str(e)

        diff = False
        if py_status != go_status:
            diff = True

        # normalize location headers
        if py_location and py_location.startswith(args.py_base):
            py_location = py_location[len(args.py_base) :]
        if go_location and go_location.startswith(args.go_base):
            go_location = go_location[len(args.go_base) :]

        if py_location != go_location:
            diff = True

        if diff:
            has_diff = True

        results.append(
            {
                "route": route,
                "py": {"status": py_status, "location": py_location},
                "go": {"status": go_status, "location": go_location},
                "match": not diff,
            }
        )

    with open(args.out, "w") as f:
        json.dump(results, f, indent=2)

    if has_diff:
        print("Differences found!")
        sys.exit(1)
    else:
        print("All routes match perfectly.")
        sys.exit(0)


if __name__ == "__main__":
    main()
