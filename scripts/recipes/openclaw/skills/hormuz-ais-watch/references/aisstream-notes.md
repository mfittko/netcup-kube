# aisstream.io notes (skill)

- WebSocket endpoint: `wss://stream.aisstream.io/v0/stream`
- Subscribe by sending JSON after connect.

Typical subscribe payload fields used by this skill:

```json
{
  "APIKey": "...",
  "BoundingBoxes": [ [ [25.5, 56.0], [27.5, 57.5] ] ],
  "FilterMessageTypes": ["PositionReport"]
}
```

Messages are JSON; useful keys vary by `MessageType`.
This skill listens for PositionReport variants and extracts:
- `MetaData.MMSI`
- `MetaData.ShipName` (if present)
- `Message.PositionReport.Latitude/Longitude/Sog`
