// DO NOT EDIT.
// swift-format-ignore-file
// swiftlint:disable all
//
// Hand-written SwiftProtobuf stubs for agent/v1/agent.proto
// Source: agent/v1/agent.proto

import SwiftProtobuf

fileprivate struct _GeneratedWithProtocGenSwiftVersion: SwiftProtobuf.ProtobufAPIVersionCheck {
  struct _2: SwiftProtobuf.ProtobufAPIVersion_2 {}
  typealias Version = _2
}

fileprivate let _protobuf_package = "agent.v1"

// MARK: - Enums

enum Agent_V1_EventType: SwiftProtobuf.Enum, Swift.CaseIterable {
  typealias RawValue = Int
  case unspecified // = 0
  case created // = 1
  case updated // = 2
  case deleted // = 3
  case UNRECOGNIZED(Int)

  init() { self = .unspecified }

  init?(rawValue: Int) {
    switch rawValue {
    case 0: self = .unspecified
    case 1: self = .created
    case 2: self = .updated
    case 3: self = .deleted
    default: self = .UNRECOGNIZED(rawValue)
    }
  }

  var rawValue: Int {
    switch self {
    case .unspecified: return 0
    case .created: return 1
    case .updated: return 2
    case .deleted: return 3
    case .UNRECOGNIZED(let i): return i
    }
  }

  static let allCases: [Agent_V1_EventType] = [
    .unspecified,
    .created,
    .updated,
    .deleted,
  ]
}

// MARK: - Messages

struct Agent_V1_ListArtifactsRequest: Sendable {
  var limit: Int32 = 0
  var offset: Int32 = 0
  var kinds: [Artifact_V1_ArtifactKind] = []
  var unknownFields = SwiftProtobuf.UnknownStorage()
  init() {}
}

struct Agent_V1_ListArtifactsResponse: Sendable {
  var artifacts: [Artifact_V1_Artifact] = []
  var unknownFields = SwiftProtobuf.UnknownStorage()
  init() {}
}

struct Agent_V1_GetArtifactRequest: Sendable {
  var id: String = String()
  var unknownFields = SwiftProtobuf.UnknownStorage()
  init() {}
}

struct Agent_V1_GetArtifactResponse: Sendable {
  var artifact: Artifact_V1_Artifact {
    get { _artifact ?? Artifact_V1_Artifact() }
    set { _artifact = newValue }
  }
  var hasArtifact: Bool { _artifact != nil }
  mutating func clearArtifact() { _artifact = nil }

  var related: [Artifact_V1_Artifact] = []
  var unknownFields = SwiftProtobuf.UnknownStorage()
  init() {}
  fileprivate var _artifact: Artifact_V1_Artifact? = nil
}

struct Agent_V1_MarkDoneRequest: Sendable {
  var id: String = String()
  var unknownFields = SwiftProtobuf.UnknownStorage()
  init() {}
}

struct Agent_V1_MarkDoneResponse: Sendable {
  var ok: Bool = false
  var unknownFields = SwiftProtobuf.UnknownStorage()
  init() {}
}

struct Agent_V1_SearchRequest: Sendable {
  var query: String = String()
  var limit: Int32 = 0
  var kinds: [Artifact_V1_ArtifactKind] = []
  var unknownFields = SwiftProtobuf.UnknownStorage()
  init() {}
}

struct Agent_V1_SearchResponse: Sendable {
  var artifacts: [Artifact_V1_Artifact] = []
  var unknownFields = SwiftProtobuf.UnknownStorage()
  init() {}
}

struct Agent_V1_SubscribeRequest: Sendable {
  var unknownFields = SwiftProtobuf.UnknownStorage()
  init() {}
}

struct Agent_V1_ArtifactEvent: Sendable {
  var type: Agent_V1_EventType = .unspecified

  var artifact: Artifact_V1_Artifact {
    get { _artifact ?? Artifact_V1_Artifact() }
    set { _artifact = newValue }
  }
  var hasArtifact: Bool { _artifact != nil }
  mutating func clearArtifact() { _artifact = nil }

  var unknownFields = SwiftProtobuf.UnknownStorage()
  init() {}
  fileprivate var _artifact: Artifact_V1_Artifact? = nil
}

struct Agent_V1_GetSystemThemeRequest: Sendable {
  var unknownFields = SwiftProtobuf.UnknownStorage()
  init() {}
}

struct Agent_V1_GetSystemThemeResponse: Sendable {
  var dark: Bool = false
  var unknownFields = SwiftProtobuf.UnknownStorage()
  init() {}
}

struct Agent_V1_GetStatusRequest: Sendable {
  var unknownFields = SwiftProtobuf.UnknownStorage()
  init() {}
}

struct Agent_V1_ComponentStatus: Sendable {
  var connected: Bool = false
  var state: String = String()
  var uptimeSeconds: Int64 = 0
  var fault: String = String()
  var unknownFields = SwiftProtobuf.UnknownStorage()
  init() {}
}

struct Agent_V1_GetStatusResponse: Sendable {
  var agent: Agent_V1_ComponentStatus {
    get { _agent ?? Agent_V1_ComponentStatus() }
    set { _agent = newValue }
  }
  var hasAgent: Bool { _agent != nil }
  mutating func clearAgent() { _agent = nil }

  var server: Agent_V1_ComponentStatus {
    get { _server ?? Agent_V1_ComponentStatus() }
    set { _server = newValue }
  }
  var hasServer: Bool { _server != nil }
  mutating func clearServer() { _server = nil }

  var native: Agent_V1_ComponentStatus {
    get { _native ?? Agent_V1_ComponentStatus() }
    set { _native = newValue }
  }
  var hasNative: Bool { _native != nil }
  mutating func clearNative() { _native = nil }

  var artifactCounts: [String: Int32] = [:]
  var unknownFields = SwiftProtobuf.UnknownStorage()
  init() {}
  fileprivate var _agent: Agent_V1_ComponentStatus? = nil
  fileprivate var _server: Agent_V1_ComponentStatus? = nil
  fileprivate var _native: Agent_V1_ComponentStatus? = nil
}

struct Agent_V1_UpdateReminderRequest: Sendable {
  var id: String = String()
  var title: String = String()
  var notes: String = String()

  var dueDate: SwiftProtobuf.Google_Protobuf_Timestamp {
    get { _dueDate ?? SwiftProtobuf.Google_Protobuf_Timestamp() }
    set { _dueDate = newValue }
  }
  var hasDueDate: Bool { _dueDate != nil }
  mutating func clearDueDate() { _dueDate = nil }

  var unknownFields = SwiftProtobuf.UnknownStorage()
  init() {}
  fileprivate var _dueDate: SwiftProtobuf.Google_Protobuf_Timestamp? = nil
}

struct Agent_V1_UpdateReminderResponse: Sendable {
  var ok: Bool = false
  var error: String = String()
  var unknownFields = SwiftProtobuf.UnknownStorage()
  init() {}
}

// MARK: - SwiftProtobuf.Message conformances

extension Agent_V1_EventType: SwiftProtobuf._ProtoNameProviding {
  static let _protobuf_nameMap: SwiftProtobuf._NameMap = [
    0: .same(proto: "EVENT_TYPE_UNSPECIFIED"),
    1: .same(proto: "EVENT_TYPE_CREATED"),
    2: .same(proto: "EVENT_TYPE_UPDATED"),
    3: .same(proto: "EVENT_TYPE_DELETED"),
  ]
}

extension Agent_V1_ListArtifactsRequest: SwiftProtobuf.Message, SwiftProtobuf._MessageImplementationBase, SwiftProtobuf._ProtoNameProviding {
  static let protoMessageName: String = _protobuf_package + ".ListArtifactsRequest"
  static let _protobuf_nameMap: SwiftProtobuf._NameMap = [
    1: .same(proto: "limit"),
    2: .same(proto: "offset"),
    3: .same(proto: "kinds"),
  ]

  mutating func decodeMessage<D: SwiftProtobuf.Decoder>(decoder: inout D) throws {
    while let fieldNumber = try decoder.nextFieldNumber() {
      switch fieldNumber {
      case 1: try { try decoder.decodeSingularInt32Field(value: &self.limit) }()
      case 2: try { try decoder.decodeSingularInt32Field(value: &self.offset) }()
      case 3: try { try decoder.decodeRepeatedEnumField(value: &self.kinds) }()
      default: break
      }
    }
  }

  func traverse<V: SwiftProtobuf.Visitor>(visitor: inout V) throws {
    if self.limit != 0 { try visitor.visitSingularInt32Field(value: self.limit, fieldNumber: 1) }
    if self.offset != 0 { try visitor.visitSingularInt32Field(value: self.offset, fieldNumber: 2) }
    if !self.kinds.isEmpty { try visitor.visitRepeatedEnumField(value: self.kinds, fieldNumber: 3) }
    try unknownFields.traverse(visitor: &visitor)
  }

  static func ==(lhs: Agent_V1_ListArtifactsRequest, rhs: Agent_V1_ListArtifactsRequest) -> Bool {
    if lhs.limit != rhs.limit { return false }
    if lhs.offset != rhs.offset { return false }
    if lhs.kinds != rhs.kinds { return false }
    if lhs.unknownFields != rhs.unknownFields { return false }
    return true
  }
}

extension Agent_V1_ListArtifactsResponse: SwiftProtobuf.Message, SwiftProtobuf._MessageImplementationBase, SwiftProtobuf._ProtoNameProviding {
  static let protoMessageName: String = _protobuf_package + ".ListArtifactsResponse"
  static let _protobuf_nameMap: SwiftProtobuf._NameMap = [
    1: .same(proto: "artifacts"),
  ]

  mutating func decodeMessage<D: SwiftProtobuf.Decoder>(decoder: inout D) throws {
    while let fieldNumber = try decoder.nextFieldNumber() {
      switch fieldNumber {
      case 1: try { try decoder.decodeRepeatedMessageField(value: &self.artifacts) }()
      default: break
      }
    }
  }

  func traverse<V: SwiftProtobuf.Visitor>(visitor: inout V) throws {
    if !self.artifacts.isEmpty { try visitor.visitRepeatedMessageField(value: self.artifacts, fieldNumber: 1) }
    try unknownFields.traverse(visitor: &visitor)
  }

  static func ==(lhs: Agent_V1_ListArtifactsResponse, rhs: Agent_V1_ListArtifactsResponse) -> Bool {
    if lhs.artifacts != rhs.artifacts { return false }
    if lhs.unknownFields != rhs.unknownFields { return false }
    return true
  }
}

extension Agent_V1_GetArtifactRequest: SwiftProtobuf.Message, SwiftProtobuf._MessageImplementationBase, SwiftProtobuf._ProtoNameProviding {
  static let protoMessageName: String = _protobuf_package + ".GetArtifactRequest"
  static let _protobuf_nameMap: SwiftProtobuf._NameMap = [
    1: .same(proto: "id"),
  ]

  mutating func decodeMessage<D: SwiftProtobuf.Decoder>(decoder: inout D) throws {
    while let fieldNumber = try decoder.nextFieldNumber() {
      switch fieldNumber {
      case 1: try { try decoder.decodeSingularStringField(value: &self.id) }()
      default: break
      }
    }
  }

  func traverse<V: SwiftProtobuf.Visitor>(visitor: inout V) throws {
    if !self.id.isEmpty { try visitor.visitSingularStringField(value: self.id, fieldNumber: 1) }
    try unknownFields.traverse(visitor: &visitor)
  }

  static func ==(lhs: Agent_V1_GetArtifactRequest, rhs: Agent_V1_GetArtifactRequest) -> Bool {
    if lhs.id != rhs.id { return false }
    if lhs.unknownFields != rhs.unknownFields { return false }
    return true
  }
}

extension Agent_V1_GetArtifactResponse: SwiftProtobuf.Message, SwiftProtobuf._MessageImplementationBase, SwiftProtobuf._ProtoNameProviding {
  static let protoMessageName: String = _protobuf_package + ".GetArtifactResponse"
  static let _protobuf_nameMap: SwiftProtobuf._NameMap = [
    1: .same(proto: "artifact"),
    2: .same(proto: "related"),
  ]

  mutating func decodeMessage<D: SwiftProtobuf.Decoder>(decoder: inout D) throws {
    while let fieldNumber = try decoder.nextFieldNumber() {
      switch fieldNumber {
      case 1: try { try decoder.decodeSingularMessageField(value: &self._artifact) }()
      case 2: try { try decoder.decodeRepeatedMessageField(value: &self.related) }()
      default: break
      }
    }
  }

  func traverse<V: SwiftProtobuf.Visitor>(visitor: inout V) throws {
    try { if let v = self._artifact { try visitor.visitSingularMessageField(value: v, fieldNumber: 1) } }()
    if !self.related.isEmpty { try visitor.visitRepeatedMessageField(value: self.related, fieldNumber: 2) }
    try unknownFields.traverse(visitor: &visitor)
  }

  static func ==(lhs: Agent_V1_GetArtifactResponse, rhs: Agent_V1_GetArtifactResponse) -> Bool {
    if lhs._artifact != rhs._artifact { return false }
    if lhs.related != rhs.related { return false }
    if lhs.unknownFields != rhs.unknownFields { return false }
    return true
  }
}

extension Agent_V1_MarkDoneRequest: SwiftProtobuf.Message, SwiftProtobuf._MessageImplementationBase, SwiftProtobuf._ProtoNameProviding {
  static let protoMessageName: String = _protobuf_package + ".MarkDoneRequest"
  static let _protobuf_nameMap: SwiftProtobuf._NameMap = [
    1: .same(proto: "id"),
  ]

  mutating func decodeMessage<D: SwiftProtobuf.Decoder>(decoder: inout D) throws {
    while let fieldNumber = try decoder.nextFieldNumber() {
      switch fieldNumber {
      case 1: try { try decoder.decodeSingularStringField(value: &self.id) }()
      default: break
      }
    }
  }

  func traverse<V: SwiftProtobuf.Visitor>(visitor: inout V) throws {
    if !self.id.isEmpty { try visitor.visitSingularStringField(value: self.id, fieldNumber: 1) }
    try unknownFields.traverse(visitor: &visitor)
  }

  static func ==(lhs: Agent_V1_MarkDoneRequest, rhs: Agent_V1_MarkDoneRequest) -> Bool {
    if lhs.id != rhs.id { return false }
    if lhs.unknownFields != rhs.unknownFields { return false }
    return true
  }
}

extension Agent_V1_MarkDoneResponse: SwiftProtobuf.Message, SwiftProtobuf._MessageImplementationBase, SwiftProtobuf._ProtoNameProviding {
  static let protoMessageName: String = _protobuf_package + ".MarkDoneResponse"
  static let _protobuf_nameMap: SwiftProtobuf._NameMap = [
    1: .same(proto: "ok"),
  ]

  mutating func decodeMessage<D: SwiftProtobuf.Decoder>(decoder: inout D) throws {
    while let fieldNumber = try decoder.nextFieldNumber() {
      switch fieldNumber {
      case 1: try { try decoder.decodeSingularBoolField(value: &self.ok) }()
      default: break
      }
    }
  }

  func traverse<V: SwiftProtobuf.Visitor>(visitor: inout V) throws {
    if self.ok != false { try visitor.visitSingularBoolField(value: self.ok, fieldNumber: 1) }
    try unknownFields.traverse(visitor: &visitor)
  }

  static func ==(lhs: Agent_V1_MarkDoneResponse, rhs: Agent_V1_MarkDoneResponse) -> Bool {
    if lhs.ok != rhs.ok { return false }
    if lhs.unknownFields != rhs.unknownFields { return false }
    return true
  }
}

extension Agent_V1_SearchRequest: SwiftProtobuf.Message, SwiftProtobuf._MessageImplementationBase, SwiftProtobuf._ProtoNameProviding {
  static let protoMessageName: String = _protobuf_package + ".SearchRequest"
  static let _protobuf_nameMap: SwiftProtobuf._NameMap = [
    1: .same(proto: "query"),
    2: .same(proto: "limit"),
    3: .same(proto: "kinds"),
  ]

  mutating func decodeMessage<D: SwiftProtobuf.Decoder>(decoder: inout D) throws {
    while let fieldNumber = try decoder.nextFieldNumber() {
      switch fieldNumber {
      case 1: try { try decoder.decodeSingularStringField(value: &self.query) }()
      case 2: try { try decoder.decodeSingularInt32Field(value: &self.limit) }()
      case 3: try { try decoder.decodeRepeatedEnumField(value: &self.kinds) }()
      default: break
      }
    }
  }

  func traverse<V: SwiftProtobuf.Visitor>(visitor: inout V) throws {
    if !self.query.isEmpty { try visitor.visitSingularStringField(value: self.query, fieldNumber: 1) }
    if self.limit != 0 { try visitor.visitSingularInt32Field(value: self.limit, fieldNumber: 2) }
    if !self.kinds.isEmpty { try visitor.visitRepeatedEnumField(value: self.kinds, fieldNumber: 3) }
    try unknownFields.traverse(visitor: &visitor)
  }

  static func ==(lhs: Agent_V1_SearchRequest, rhs: Agent_V1_SearchRequest) -> Bool {
    if lhs.query != rhs.query { return false }
    if lhs.limit != rhs.limit { return false }
    if lhs.kinds != rhs.kinds { return false }
    if lhs.unknownFields != rhs.unknownFields { return false }
    return true
  }
}

extension Agent_V1_SearchResponse: SwiftProtobuf.Message, SwiftProtobuf._MessageImplementationBase, SwiftProtobuf._ProtoNameProviding {
  static let protoMessageName: String = _protobuf_package + ".SearchResponse"
  static let _protobuf_nameMap: SwiftProtobuf._NameMap = [
    1: .same(proto: "artifacts"),
  ]

  mutating func decodeMessage<D: SwiftProtobuf.Decoder>(decoder: inout D) throws {
    while let fieldNumber = try decoder.nextFieldNumber() {
      switch fieldNumber {
      case 1: try { try decoder.decodeRepeatedMessageField(value: &self.artifacts) }()
      default: break
      }
    }
  }

  func traverse<V: SwiftProtobuf.Visitor>(visitor: inout V) throws {
    if !self.artifacts.isEmpty { try visitor.visitRepeatedMessageField(value: self.artifacts, fieldNumber: 1) }
    try unknownFields.traverse(visitor: &visitor)
  }

  static func ==(lhs: Agent_V1_SearchResponse, rhs: Agent_V1_SearchResponse) -> Bool {
    if lhs.artifacts != rhs.artifacts { return false }
    if lhs.unknownFields != rhs.unknownFields { return false }
    return true
  }
}

extension Agent_V1_SubscribeRequest: SwiftProtobuf.Message, SwiftProtobuf._MessageImplementationBase, SwiftProtobuf._ProtoNameProviding {
  static let protoMessageName: String = _protobuf_package + ".SubscribeRequest"
  static let _protobuf_nameMap = SwiftProtobuf._NameMap()

  mutating func decodeMessage<D: SwiftProtobuf.Decoder>(decoder: inout D) throws {
    while try decoder.nextFieldNumber() != nil {}
  }

  func traverse<V: SwiftProtobuf.Visitor>(visitor: inout V) throws {
    try unknownFields.traverse(visitor: &visitor)
  }

  static func ==(lhs: Agent_V1_SubscribeRequest, rhs: Agent_V1_SubscribeRequest) -> Bool {
    if lhs.unknownFields != rhs.unknownFields { return false }
    return true
  }
}

extension Agent_V1_ArtifactEvent: SwiftProtobuf.Message, SwiftProtobuf._MessageImplementationBase, SwiftProtobuf._ProtoNameProviding {
  static let protoMessageName: String = _protobuf_package + ".ArtifactEvent"
  static let _protobuf_nameMap: SwiftProtobuf._NameMap = [
    1: .same(proto: "type"),
    2: .same(proto: "artifact"),
  ]

  mutating func decodeMessage<D: SwiftProtobuf.Decoder>(decoder: inout D) throws {
    while let fieldNumber = try decoder.nextFieldNumber() {
      switch fieldNumber {
      case 1: try { try decoder.decodeSingularEnumField(value: &self.type) }()
      case 2: try { try decoder.decodeSingularMessageField(value: &self._artifact) }()
      default: break
      }
    }
  }

  func traverse<V: SwiftProtobuf.Visitor>(visitor: inout V) throws {
    if self.type != .unspecified { try visitor.visitSingularEnumField(value: self.type, fieldNumber: 1) }
    try { if let v = self._artifact { try visitor.visitSingularMessageField(value: v, fieldNumber: 2) } }()
    try unknownFields.traverse(visitor: &visitor)
  }

  static func ==(lhs: Agent_V1_ArtifactEvent, rhs: Agent_V1_ArtifactEvent) -> Bool {
    if lhs.type != rhs.type { return false }
    if lhs._artifact != rhs._artifact { return false }
    if lhs.unknownFields != rhs.unknownFields { return false }
    return true
  }
}

extension Agent_V1_GetSystemThemeRequest: SwiftProtobuf.Message, SwiftProtobuf._MessageImplementationBase, SwiftProtobuf._ProtoNameProviding {
  static let protoMessageName: String = _protobuf_package + ".GetSystemThemeRequest"
  static let _protobuf_nameMap = SwiftProtobuf._NameMap()

  mutating func decodeMessage<D: SwiftProtobuf.Decoder>(decoder: inout D) throws {
    while try decoder.nextFieldNumber() != nil {}
  }

  func traverse<V: SwiftProtobuf.Visitor>(visitor: inout V) throws {
    try unknownFields.traverse(visitor: &visitor)
  }

  static func ==(lhs: Agent_V1_GetSystemThemeRequest, rhs: Agent_V1_GetSystemThemeRequest) -> Bool {
    if lhs.unknownFields != rhs.unknownFields { return false }
    return true
  }
}

extension Agent_V1_GetSystemThemeResponse: SwiftProtobuf.Message, SwiftProtobuf._MessageImplementationBase, SwiftProtobuf._ProtoNameProviding {
  static let protoMessageName: String = _protobuf_package + ".GetSystemThemeResponse"
  static let _protobuf_nameMap: SwiftProtobuf._NameMap = [
    1: .same(proto: "dark"),
  ]

  mutating func decodeMessage<D: SwiftProtobuf.Decoder>(decoder: inout D) throws {
    while let fieldNumber = try decoder.nextFieldNumber() {
      switch fieldNumber {
      case 1: try { try decoder.decodeSingularBoolField(value: &self.dark) }()
      default: break
      }
    }
  }

  func traverse<V: SwiftProtobuf.Visitor>(visitor: inout V) throws {
    if self.dark != false { try visitor.visitSingularBoolField(value: self.dark, fieldNumber: 1) }
    try unknownFields.traverse(visitor: &visitor)
  }

  static func ==(lhs: Agent_V1_GetSystemThemeResponse, rhs: Agent_V1_GetSystemThemeResponse) -> Bool {
    if lhs.dark != rhs.dark { return false }
    if lhs.unknownFields != rhs.unknownFields { return false }
    return true
  }
}

extension Agent_V1_GetStatusRequest: SwiftProtobuf.Message, SwiftProtobuf._MessageImplementationBase, SwiftProtobuf._ProtoNameProviding {
  static let protoMessageName: String = _protobuf_package + ".GetStatusRequest"
  static let _protobuf_nameMap = SwiftProtobuf._NameMap()

  mutating func decodeMessage<D: SwiftProtobuf.Decoder>(decoder: inout D) throws {
    while try decoder.nextFieldNumber() != nil {}
  }

  func traverse<V: SwiftProtobuf.Visitor>(visitor: inout V) throws {
    try unknownFields.traverse(visitor: &visitor)
  }

  static func ==(lhs: Agent_V1_GetStatusRequest, rhs: Agent_V1_GetStatusRequest) -> Bool {
    if lhs.unknownFields != rhs.unknownFields { return false }
    return true
  }
}

extension Agent_V1_ComponentStatus: SwiftProtobuf.Message, SwiftProtobuf._MessageImplementationBase, SwiftProtobuf._ProtoNameProviding {
  static let protoMessageName: String = _protobuf_package + ".ComponentStatus"
  static let _protobuf_nameMap: SwiftProtobuf._NameMap = [
    1: .same(proto: "connected"),
    2: .same(proto: "state"),
    3: .standard(proto: "uptime_seconds"),
    4: .same(proto: "fault"),
  ]

  mutating func decodeMessage<D: SwiftProtobuf.Decoder>(decoder: inout D) throws {
    while let fieldNumber = try decoder.nextFieldNumber() {
      switch fieldNumber {
      case 1: try { try decoder.decodeSingularBoolField(value: &self.connected) }()
      case 2: try { try decoder.decodeSingularStringField(value: &self.state) }()
      case 3: try { try decoder.decodeSingularInt64Field(value: &self.uptimeSeconds) }()
      case 4: try { try decoder.decodeSingularStringField(value: &self.fault) }()
      default: break
      }
    }
  }

  func traverse<V: SwiftProtobuf.Visitor>(visitor: inout V) throws {
    if self.connected != false { try visitor.visitSingularBoolField(value: self.connected, fieldNumber: 1) }
    if !self.state.isEmpty { try visitor.visitSingularStringField(value: self.state, fieldNumber: 2) }
    if self.uptimeSeconds != 0 { try visitor.visitSingularInt64Field(value: self.uptimeSeconds, fieldNumber: 3) }
    if !self.fault.isEmpty { try visitor.visitSingularStringField(value: self.fault, fieldNumber: 4) }
    try unknownFields.traverse(visitor: &visitor)
  }

  static func ==(lhs: Agent_V1_ComponentStatus, rhs: Agent_V1_ComponentStatus) -> Bool {
    if lhs.connected != rhs.connected { return false }
    if lhs.state != rhs.state { return false }
    if lhs.uptimeSeconds != rhs.uptimeSeconds { return false }
    if lhs.fault != rhs.fault { return false }
    if lhs.unknownFields != rhs.unknownFields { return false }
    return true
  }
}

extension Agent_V1_GetStatusResponse: SwiftProtobuf.Message, SwiftProtobuf._MessageImplementationBase, SwiftProtobuf._ProtoNameProviding {
  static let protoMessageName: String = _protobuf_package + ".GetStatusResponse"
  static let _protobuf_nameMap: SwiftProtobuf._NameMap = [
    1: .same(proto: "agent"),
    2: .same(proto: "server"),
    3: .same(proto: "native"),
    4: .standard(proto: "artifact_counts"),
  ]

  mutating func decodeMessage<D: SwiftProtobuf.Decoder>(decoder: inout D) throws {
    while let fieldNumber = try decoder.nextFieldNumber() {
      switch fieldNumber {
      case 1: try { try decoder.decodeSingularMessageField(value: &self._agent) }()
      case 2: try { try decoder.decodeSingularMessageField(value: &self._server) }()
      case 3: try { try decoder.decodeSingularMessageField(value: &self._native) }()
      case 4: try { try decoder.decodeMapField(fieldType: SwiftProtobuf._ProtobufMap<SwiftProtobuf.ProtobufString, SwiftProtobuf.ProtobufInt32>.self, value: &self.artifactCounts) }()
      default: break
      }
    }
  }

  func traverse<V: SwiftProtobuf.Visitor>(visitor: inout V) throws {
    try { if let v = self._agent { try visitor.visitSingularMessageField(value: v, fieldNumber: 1) } }()
    try { if let v = self._server { try visitor.visitSingularMessageField(value: v, fieldNumber: 2) } }()
    try { if let v = self._native { try visitor.visitSingularMessageField(value: v, fieldNumber: 3) } }()
    if !self.artifactCounts.isEmpty {
      try visitor.visitMapField(fieldType: SwiftProtobuf._ProtobufMap<SwiftProtobuf.ProtobufString, SwiftProtobuf.ProtobufInt32>.self, value: self.artifactCounts, fieldNumber: 4)
    }
    try unknownFields.traverse(visitor: &visitor)
  }

  static func ==(lhs: Agent_V1_GetStatusResponse, rhs: Agent_V1_GetStatusResponse) -> Bool {
    if lhs._agent != rhs._agent { return false }
    if lhs._server != rhs._server { return false }
    if lhs._native != rhs._native { return false }
    if lhs.artifactCounts != rhs.artifactCounts { return false }
    if lhs.unknownFields != rhs.unknownFields { return false }
    return true
  }
}

extension Agent_V1_UpdateReminderRequest: SwiftProtobuf.Message, SwiftProtobuf._MessageImplementationBase, SwiftProtobuf._ProtoNameProviding {
  static let protoMessageName: String = _protobuf_package + ".UpdateReminderRequest"
  static let _protobuf_nameMap: SwiftProtobuf._NameMap = [
    1: .same(proto: "id"),
    2: .same(proto: "title"),
    3: .same(proto: "notes"),
    4: .standard(proto: "due_date"),
  ]

  mutating func decodeMessage<D: SwiftProtobuf.Decoder>(decoder: inout D) throws {
    while let fieldNumber = try decoder.nextFieldNumber() {
      switch fieldNumber {
      case 1: try { try decoder.decodeSingularStringField(value: &self.id) }()
      case 2: try { try decoder.decodeSingularStringField(value: &self.title) }()
      case 3: try { try decoder.decodeSingularStringField(value: &self.notes) }()
      case 4: try { try decoder.decodeSingularMessageField(value: &self._dueDate) }()
      default: break
      }
    }
  }

  func traverse<V: SwiftProtobuf.Visitor>(visitor: inout V) throws {
    if !self.id.isEmpty { try visitor.visitSingularStringField(value: self.id, fieldNumber: 1) }
    if !self.title.isEmpty { try visitor.visitSingularStringField(value: self.title, fieldNumber: 2) }
    if !self.notes.isEmpty { try visitor.visitSingularStringField(value: self.notes, fieldNumber: 3) }
    try { if let v = self._dueDate { try visitor.visitSingularMessageField(value: v, fieldNumber: 4) } }()
    try unknownFields.traverse(visitor: &visitor)
  }

  static func ==(lhs: Agent_V1_UpdateReminderRequest, rhs: Agent_V1_UpdateReminderRequest) -> Bool {
    if lhs.id != rhs.id { return false }
    if lhs.title != rhs.title { return false }
    if lhs.notes != rhs.notes { return false }
    if lhs._dueDate != rhs._dueDate { return false }
    if lhs.unknownFields != rhs.unknownFields { return false }
    return true
  }
}

extension Agent_V1_UpdateReminderResponse: SwiftProtobuf.Message, SwiftProtobuf._MessageImplementationBase, SwiftProtobuf._ProtoNameProviding {
  static let protoMessageName: String = _protobuf_package + ".UpdateReminderResponse"
  static let _protobuf_nameMap: SwiftProtobuf._NameMap = [
    1: .same(proto: "ok"),
    2: .same(proto: "error"),
  ]

  mutating func decodeMessage<D: SwiftProtobuf.Decoder>(decoder: inout D) throws {
    while let fieldNumber = try decoder.nextFieldNumber() {
      switch fieldNumber {
      case 1: try { try decoder.decodeSingularBoolField(value: &self.ok) }()
      case 2: try { try decoder.decodeSingularStringField(value: &self.error) }()
      default: break
      }
    }
  }

  func traverse<V: SwiftProtobuf.Visitor>(visitor: inout V) throws {
    if self.ok != false { try visitor.visitSingularBoolField(value: self.ok, fieldNumber: 1) }
    if !self.error.isEmpty { try visitor.visitSingularStringField(value: self.error, fieldNumber: 2) }
    try unknownFields.traverse(visitor: &visitor)
  }

  static func ==(lhs: Agent_V1_UpdateReminderResponse, rhs: Agent_V1_UpdateReminderResponse) -> Bool {
    if lhs.ok != rhs.ok { return false }
    if lhs.error != rhs.error { return false }
    if lhs.unknownFields != rhs.unknownFields { return false }
    return true
  }
}
