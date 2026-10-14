import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';

/// Captures the outgoing request and returns a canned response, so the token
/// API can be exercised without touching the network.
class FakeHttpAdapter implements HttpClientAdapter {
  FakeHttpAdapter(this._respond);

  final ResponseBody Function(RequestOptions options) _respond;

  RequestOptions? lastRequest;
  String? lastRequestBody;
  bool closed = false;

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    lastRequest = options;
    if (requestStream != null) {
      final chunks = await requestStream.toList();
      lastRequestBody = utf8.decode(chunks.expand((chunk) => chunk).toList());
    }
    return _respond(options);
  }

  @override
  void close({bool force = false}) {
    closed = true;
  }
}

/// Builds a JSON [ResponseBody] the Dio transformer will decode into a `Map`.
ResponseBody jsonResponse(Map<String, dynamic> body, {int statusCode = 200}) {
  return ResponseBody.fromString(
    jsonEncode(body),
    statusCode,
    headers: {
      Headers.contentTypeHeader: [Headers.jsonContentType],
    },
  );
}
