class GetTokenResponse {
  final String? token;
  final String? status;
  final String? message;

  GetTokenResponse({
    this.token,
    this.status,
    this.message,
  });

  factory GetTokenResponse.fromJson(Map<String, dynamic> json) {
    return GetTokenResponse(
      token: json['ACCESS_TOKEN'] as String?,
      status: json['STATUS'] as String?,
      message: json['MESSAGE'] as String?,
    );
  }

  Map<String, dynamic> toJson() {
    return {
      if (token != null) 'ACCESS_TOKEN': token,
      if (status != null) 'STATUS': status,
      if (message != null) 'MESSAGE': message,
    };
  }
}
