import 'package:flutter/material.dart';
import 'package:payfast_flutter_sdk/payfast_flutter_sdk.dart';
import 'package:shared_preferences/shared_preferences.dart';
import '../network/api_endpoints.dart';
import 'session_registry.dart';

/// Result object encapsulating the outcome of a PayFast transaction.
class PayfastPaymentResult {
  const PayfastPaymentResult({
    required this.success,
    required this.basketId,
    this.isCancelled = false,
    this.errorMessage,
  });

  factory PayfastPaymentResult.success(String basketId) {
    return PayfastPaymentResult(
      success: true,
      basketId: basketId,
    );
  }

  factory PayfastPaymentResult.failed(String basketId, {String? errorMessage}) {
    return PayfastPaymentResult(
      success: false,
      basketId: basketId,
      errorMessage: errorMessage ?? 'Payment failed',
    );
  }

  factory PayfastPaymentResult.cancelled(String basketId, {String? errorMessage}) {
    return PayfastPaymentResult(
      success: false,
      isCancelled: true,
      basketId: basketId,
      errorMessage: errorMessage ?? 'Payment was cancelled by the user',
    );
  }

  final bool success;
  final bool isCancelled;
  final String? errorMessage;
  final String basketId;
}

/// Service that coordinates PayFast payment gateway execution using the
/// official PayFast Flutter SDK (`payfast_flutter_sdk`).
///
/// Handles token generation, hosted gateway redirection, 3D-Secure
/// verification, and response capture.
class PayfastService {
  PayfastService({
    this.merchantId = defaultMerchantId,
    this.securedKey = defaultSecuredKey,
    this.merchantName = defaultMerchantName,
    this.isUAT = defaultIsUAT,
    String? successUrl,
    String? failureUrl,
    String? checkoutUrl,
  })  : successUrl = successUrl ?? defaultSuccessUrl,
        failureUrl = failureUrl ?? defaultFailureUrl,
        checkoutUrl = checkoutUrl ?? ApiEndpoints.payfastIPN();

  static const String defaultMerchantId =
      String.fromEnvironment('PAYFAST_MERCHANT_ID', defaultValue: '102');
  static const String defaultSecuredKey =
      String.fromEnvironment('PAYFAST_SECURED_KEY', defaultValue: 'zWHjBp2AlttNu1sK');
  static const String defaultMerchantName =
      String.fromEnvironment('PAYFAST_MERCHANT_NAME', defaultValue: 'OMNIGO');
  static const bool defaultIsUAT =
      bool.fromEnvironment('PAYFAST_IS_UAT', defaultValue: true);
  static String get defaultSuccessUrl =>
      const String.fromEnvironment('PAYFAST_SUCCESS_URL',
          defaultValue: 'https://omnigo-app-4-production.up.railway.app/payment/success');
  static String get defaultFailureUrl =>
      const String.fromEnvironment('PAYFAST_FAILURE_URL',
          defaultValue: 'https://omnigo-app-4-production.up.railway.app/payment/failure');

  final String merchantId;
  final String securedKey;
  final String merchantName;
  final bool isUAT;
  final String successUrl;
  final String failureUrl;
  final String checkoutUrl;

  /// Processes a payment through the PayFast Redirection SDK.
  ///
  /// Opens the official PayFast hosted WebView where customers can select
  /// payment method (Credit/Debit Card, Bank Account, EasyPaisa, JazzCash, UnionPay)
  /// and perform 3DS/OTP verification.
  ///
  /// Returns a [PayfastPaymentResult] indicating success, failure, or cancellation.
  Future<PayfastPaymentResult> processPayment({
    required BuildContext context,
    required String basketId,
    required double amount,
    String? description,
    String? customerEmail,
    String? customerMobile,
  }) async {
    // 1. Resolve customer details with fallbacks
    String resolvedEmail = (customerEmail ?? '').trim();
    String resolvedMobile = (customerMobile ?? '').trim();

    if (resolvedEmail.isEmpty || resolvedMobile.isEmpty) {
      final session = SessionRegistry.instance;
      if (resolvedEmail.isEmpty) {
        resolvedEmail = (session.email ?? '').trim();
      }
      if (resolvedMobile.isEmpty) {
        resolvedMobile = (session.phone ?? '').trim();
      }

      if (resolvedEmail.isEmpty || resolvedMobile.isEmpty) {
        try {
          final prefs = await SharedPreferences.getInstance();
          if (resolvedEmail.isEmpty) {
            resolvedEmail = (prefs.getString('email') ?? '').trim();
          }
          if (resolvedMobile.isEmpty) {
            resolvedMobile = (prefs.getString('phone') ?? '').trim();
          }
        } catch (_) {}
      }
    }

    // Standard default Pakistani merchant fallbacks if user profile lacks contact info
    if (resolvedEmail.isEmpty) {
      resolvedEmail = 'customer@omnigo.com.pk';
    }
    if (resolvedMobile.isEmpty) {
      resolvedMobile = '03001234567';
    } else {
      // Clean mobile number to standard format
      resolvedMobile = resolvedMobile.replaceAll(RegExp(r'[^0-9]'), '');
      if (resolvedMobile.startsWith('92')) {
        resolvedMobile = '0${resolvedMobile.substring(2)}';
      }
      if (resolvedMobile.isEmpty) {
        resolvedMobile = '03001234567';
      }
    }

    final orderDesc = description != null && description.isNotEmpty
        ? description
        : 'OMNIGO Order $basketId';

    // Amount formatted to 2 decimals
    final formattedAmount = amount.toStringAsFixed(2);

    bool isSuccess = false;
    bool isFailure = false;
    String? failureReason;

    try {
      final service = RedirectionService.newBuilder()
          .setMerchantId(merchantId)
          .setSecuredKey(securedKey)
          .setBasketID(basketId)
          .setTxnAmount(formattedAmount)
          .setDescription(orderDesc)
          .setCustomerEmail(resolvedEmail)
          .setCustomerMobile(resolvedMobile)
          .setMerchantName(merchantName)
          .setSuccessURL(successUrl)
          .setFailureURL(failureUrl)
          .setCheckoutURL(checkoutUrl)
          .setIsUAT(isUAT)
          .setSuccessListener((url) {
            debugPrint('[PayFast] Success callback triggered: $url');
            isSuccess = true;
          })
          .setFailureListener((url) {
            debugPrint('[PayFast] Failure callback triggered: $url');
            isFailure = true;
            failureReason = 'Payment was declined or cancelled by customer.';
          })
          .setTokenErrorListener((error) {
            debugPrint('[PayFast] Token error callback triggered: $error');
            isFailure = true;
            failureReason = 'Token generation failed: $error';
          })
          .build();

      if (!context.mounted) {
        return PayfastPaymentResult.failed(basketId, errorMessage: 'Screen context lost.');
      }

      // Launch the official SDK WebView
      final modalResult = await service.fetchTokenAndOpenWebView(context);

      if (isSuccess || (modalResult is Map && modalResult['status'] == 'success')) {
        return PayfastPaymentResult.success(basketId);
      } else if (isFailure || (modalResult is Map && modalResult['status'] == 'failure')) {
        return PayfastPaymentResult.failed(
          basketId,
          errorMessage: failureReason ?? 'Payment was not approved.',
        );
      } else {
        // Modal was dismissed or closed without completing
        return PayfastPaymentResult.cancelled(
          basketId,
          errorMessage: failureReason ?? 'Payment window was closed.',
        );
      }
    } on ArgumentError catch (e) {
      debugPrint('[PayFast] ArgumentError: ${e.message}');
      return PayfastPaymentResult.failed(basketId, errorMessage: 'Invalid payment parameters: ${e.message}');
    } catch (e) {
      debugPrint('[PayFast] Unexpected error: $e');
      return PayfastPaymentResult.failed(basketId, errorMessage: 'Payment error: $e');
    }
  }
}
