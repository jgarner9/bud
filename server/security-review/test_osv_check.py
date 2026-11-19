import io
import json
import unittest
import urllib.error

from osv_check import check_package


class Response(io.BytesIO):
    pass


class CheckTests(unittest.TestCase):
    item = {"name": "example", "ecosystem": "npm", "version": "1.0.0"}

    def test_affected_and_request_identity(self):
        def opener(request, timeout):
            self.assertEqual(json.loads(request.data), {"package": {"name": "example", "ecosystem": "npm"}, "version": "1.0.0"})
            self.assertEqual(timeout, 15)
            return Response(b'{"vulns":[{"id":"GHSA-example"}]}')
        result = check_package(self.item, opener)
        self.assertEqual(result["status"], "affected")
        self.assertEqual(result["advisories"][0]["id"], "GHSA-example")

    def test_empty_is_not_failure(self):
        self.assertEqual(check_package(self.item, lambda *_a, **_k: Response(b'{}'))["status"], "no_known_advisories")

    def test_failure_and_bad_response_are_unknown(self):
        def fail(*_a, **_k):
            raise urllib.error.URLError("offline")
        for opener in (fail, lambda *_a, **_k: Response(b'not-json'), lambda *_a, **_k: Response(b'{"vulns":{}}')):
            self.assertEqual(check_package(self.item, opener)["status"], "unknown")
        self.assertEqual(check_package({"name": "example"})["status"], "unknown")


if __name__ == "__main__":
    unittest.main()
