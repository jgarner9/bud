"""Query OSV for an explicit, non-secret package inventory.

A successful empty result is distinct from an unknown or failed query.
"""
import json
import urllib.error
import urllib.request

OSV_QUERY = "https://api.osv.dev/v1/query"
MAX_RESPONSE = 2 * 1024 * 1024


def check_package(item, opener=urllib.request.urlopen):
    """Return affected, no_known_advisories, or unknown for one package."""
    name, ecosystem, version = (item.get(k) for k in ("name", "ecosystem", "version"))
    if not all(isinstance(v, str) and v.strip() for v in (name, ecosystem, version)):
        return {"package": item, "status": "unknown", "error": "name, ecosystem and version are required"}
    payload = json.dumps({"package": {"name": name, "ecosystem": ecosystem}, "version": version}).encode()
    req = urllib.request.Request(OSV_QUERY, data=payload, headers={"Content-Type": "application/json"}, method="POST")
    try:
        with opener(req, timeout=15) as response:
            data = response.read(MAX_RESPONSE + 1)
        if len(data) > MAX_RESPONSE:
            raise ValueError("response exceeds limit")
        result = json.loads(data)
        if not isinstance(result, dict) or not isinstance(result.get("vulns", []), list):
            raise ValueError("invalid OSV response")
        advisories = result.get("vulns", [])
        if any(not isinstance(v, dict) or not isinstance(v.get("id"), str) or not v["id"] for v in advisories):
            raise ValueError("invalid advisory")
        return {"package": item, "status": "affected" if advisories else "no_known_advisories", "advisories": advisories}
    except (OSError, ValueError, UnicodeError) as exc:
        return {"package": item, "status": "unknown", "error": str(exc)}
