import 'dart:convert';
import 'package:dio/dio.dart';
import '../models/get_token_response.dart';

class PayfastApiService {
  static const String _uatBaseUrl = 'https://ipguat.apps.net.pk/Ecommerce/api';
  static const String _prodBaseUrl = 'https://ipg1.apps.net.pk/Ecommerce/api';

  final Dio _dio;
  final bool isUAT;

  PayfastApiService({Dio? dio, this.isUAT = true}) : _dio = dio ?? Dio();

  String get _baseUrl => isUAT ? _uatBaseUrl : _prodBaseUrl;

  /// Gets access token from Payfast API
  ///
  /// Returns [GetTokenResponse] with token on success
  /// Throws [Exception] on failure
  Future<GetTokenResponse> getToken({
    required String merchantId,
    required String securedKey,
  }) async {
    final data = {
      "MERCHANT_ID": merchantId,
      "SECURED_KEY": securedKey,
    };

    try {
      final response = await _dio.post(
        '$_baseUrl/Transaction/GetAccessToken',
        data: data,
        options: Options(
          contentType: Headers.formUrlEncodedContentType,
        ),
      );

      if (response.statusCode == 200) {
        final dynamic rawData = response.data;
        final Map<String, dynamic> jsonMap;
        if (rawData is Map<String, dynamic>) {
          jsonMap = rawData;
        } else if (rawData is Map) {
          jsonMap = Map<String, dynamic>.from(rawData);
        } else if (rawData is String) {
          jsonMap = jsonDecode(rawData) as Map<String, dynamic>;
        } else {
          throw Exception('Unexpected token response format: $rawData');
        }
        return GetTokenResponse.fromJson(jsonMap);
      } else {
        throw Exception(
          'Failed to get token: ${response.statusCode} - ${response.data}',
        );
      }
    } catch (e) {
      throw Exception('Error getting token: $e');
    }
  }

  void dispose() {
    _dio.close();
  }
}
