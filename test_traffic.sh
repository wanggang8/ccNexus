#!/bin/bash

BASE_URL="http://localhost:8787"

echo "=== Testing Traffic Recording APIs ==="
echo ""

echo "1. Check recording status:"
curl -s "${BASE_URL}/api/traffic/recording" | jq .
echo ""

echo "2. Enable recording:"
curl -s -X POST "${BASE_URL}/api/traffic/recording" \
  -H "Content-Type: application/json" \
  -d '{"recording": true}' | jq .
echo ""

echo "3. Get traffic logs:"
curl -s "${BASE_URL}/api/traffic/logs" | jq .
echo ""

echo "4. Clear logs:"
curl -s -X POST "${BASE_URL}/api/traffic/clear" | jq .
echo ""

echo "5. Verify logs cleared:"
curl -s "${BASE_URL}/api/traffic/logs" | jq .
echo ""

echo "=== Test Complete ==="
