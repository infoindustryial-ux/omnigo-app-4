class GetTokenRequest {
  final String merchantId;
  final String securedKey;

  GetTokenRequest({
    required this.merchantId,
    required this.securedKey,
  });

  Map<String, String> toFormData() {
    return {
      'MERCHANT_ID': merchantId,
      'SECURED_KEY': securedKey,
    };
  }
}
